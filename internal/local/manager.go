package local

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DanMarshall909/ai-router/internal/config"
	"github.com/DanMarshall909/ai-router/internal/routing"
	"golang.org/x/sync/singleflight"
)

const (
	localModelHealthPath = "/health"
	localModelSlotsPath  = "/slots"
)

// Manager supervises the lifecycle of a llama-server process.
type Manager struct {
	mu    sync.Mutex
	state routing.LocalModelState
	pid   int
	proc  Process

	procFac    ProcessFactory
	cfg        config.LocalModelConfig
	healthURL  string
	slotsURL   string
	httpClient *http.Client

	activeRequests atomic.Int32
	lastActivity   atomic.Int64

	sf singleflight.Group

	// exitCh receives the single exit notification from the watcher.
	exitCh chan ExitStatus

	// idleStopCh signals the idle watcher to stop.
	idleStopCh chan struct{}
}

// NewManager creates a process manager from the given config and factory.
func NewManager(cfg config.LocalModelConfig, fac ProcessFactory) *Manager {
	return &Manager{
		state:      routing.StateStopped,
		procFac:    fac,
		cfg:        cfg,
		healthURL:  fmt.Sprintf("http://%s:%d%s", cfg.Host, cfg.Port, localModelHealthPath),
		slotsURL:   fmt.Sprintf("http://%s:%d%s", cfg.Host, cfg.Port, localModelSlotsPath),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		exitCh:     make(chan ExitStatus, 1),
		idleStopCh: make(chan struct{}),
	}
}

// State returns the current lifecycle state.
func (m *Manager) State() routing.LocalModelState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Pid returns the OS process ID, or 0 if not running.
func (m *Manager) Pid() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pid
}

// IsBusy reports whether any local request is in flight.
func (m *Manager) IsBusy() bool {
	return m.activeRequests.Load() > 0
}

// RequestBegin increments the active request counter and updates last activity.
func (m *Manager) RequestBegin() {
	m.activeRequests.Add(1)
	m.lastActivity.Store(time.Now().UnixNano())
}

// RequestEnd decrements the active request counter.
func (m *Manager) RequestEnd() {
	m.activeRequests.Add(-1)
}

// UpdateActivity records the current time as last activity.
func (m *Manager) UpdateActivity() {
	m.lastActivity.Store(time.Now().UnixNano())
}

// LastActivity returns the last activity time.
func (m *Manager) LastActivity() time.Time {
	nano := m.lastActivity.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

// Start begins local model startup using singleflight to coalesce concurrent calls.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	switch m.state {
	case routing.StateReady:
		m.mu.Unlock()
		return nil
	case routing.StateStarting:
		m.mu.Unlock()
		_, err, _ := m.sf.Do("start", func() (any, error) {
			return nil, nil
		})
		return err
	case routing.StateStopping:
		m.mu.Unlock()
		return fmt.Errorf("model is stopping")
	}
	m.mu.Unlock()

	_, err, _ := m.sf.Do("start", func() (any, error) {
		return m.doStart(ctx)
	})
	return err
}

// StartInBackground starts the local model without blocking the caller.
// If the model is already started or starting, it returns immediately.
func (m *Manager) StartInBackground(ctx context.Context) {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()

	if state == routing.StateReady || state == routing.StateStarting || state == routing.StateStopping {
		return
	}

	// Use background context so startup isn't cancelled when the request ends
	go m.Start(context.Background())
}

func (m *Manager) doStart(ctx context.Context) (any, error) {
	if m.checkAdoptableHealth() {
		m.UpdateActivity()
		m.setState(routing.StateReady)
		slog.Info("adopted existing local model", "host", m.cfg.Host, "port", m.cfg.Port)
		return nil, nil
	}

	slog.Info("starting local model", "executable", m.cfg.ExecutablePath, "model", m.cfg.ModelPath)
	m.setState(routing.StateStarting)

	args := buildArgs(m.cfg)
	var proc Process
	if m.cfg.LDLibraryPath != "" {
		env := []string{"LD_LIBRARY_PATH=" + m.cfg.LDLibraryPath}
		proc = m.procFac.NewProcessWithEnv(m.cfg.ExecutablePath, env, args...)
	} else {
		proc = m.procFac.NewProcess(m.cfg.ExecutablePath, args...)
	}

	if err := proc.Start(ctx); err != nil {
		slog.Error("failed to start process", "err", err)
		m.setState(routing.StateFaulted)
		return nil, fmt.Errorf("starting process: %w", err)
	}

	m.mu.Lock()
	m.proc = proc
	m.pid = proc.Pid()
	m.mu.Unlock()

	slog.Info("process started", "pid", m.pid)

	// Single exit watcher: drains the Wait channel exactly once.
	go m.watchExit(proc)

	// Wait for readiness
	if err := m.waitForReady(ctx); err != nil {
		slog.Error("readiness check failed", "err", err)
		proc.Kill()
		<-m.exitCh
		m.setState(routing.StateFaulted)
		return nil, err
	}

	slog.Info("local model ready", "pid", m.pid)
	m.UpdateActivity()
	m.setState(routing.StateReady)
	return nil, nil
}

func buildArgs(cfg config.LocalModelConfig) []string {
	args := []string{
		"-m", cfg.ModelPath,
		"--host", cfg.Host,
		"--port", fmt.Sprintf("%d", cfg.Port),
	}
	args = append(args, cfg.AdditionalArgs...)
	return args
}

func (m *Manager) waitForReady(ctx context.Context) error {
	deadline := time.After(m.cfg.StartupTimeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("readiness timeout after %s", m.cfg.StartupTimeout)
		case <-ticker.C:
			if m.checkHealth() {
				return nil
			}
		}
	}
}

func (m *Manager) checkHealth() bool {
	resp, err := m.httpClient.Get(m.healthURL)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (m *Manager) checkAdoptableHealth() bool {
	resp, err := m.httpClient.Get(m.healthURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}

	var health struct {
		Status string `json:"status"`
	}
	return json.NewDecoder(resp.Body).Decode(&health) == nil && health.Status == "ok"
}

func (m *Manager) modelIsProcessing() bool {
	resp, err := m.httpClient.Get(m.slotsURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}

	var slots []struct {
		IsProcessing bool `json:"is_processing"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&slots); err != nil {
		return false
	}
	for _, slot := range slots {
		if slot.IsProcessing {
			return true
		}
	}
	return false
}

// watchExit reads from proc.Wait() exactly once and sends to m.exitCh.
// It detects unexpected exits when the process dies while in Ready state.
func (m *Manager) watchExit(proc Process) {
	status := <-proc.Wait()

	// Forward to exitCh for Stop to consume.
	select {
	case m.exitCh <- status:
	default:
	}

	// Detect unexpected exit (not triggered by Stop).
	m.mu.Lock()
	switch m.state {
	case routing.StateReady, routing.StateStarting:
		m.state = routing.StateFaulted
		m.pid = 0
		m.proc = nil
	}
	m.mu.Unlock()
}

// Stop gracefully stops the process, force-killing after the shutdown timeout.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.state == routing.StateStopped || m.state == routing.StateStopping {
		m.mu.Unlock()
		return nil
	}
	proc := m.proc
	m.state = routing.StateStopping
	m.mu.Unlock()

	if proc == nil {
		m.setState(routing.StateStopped)
		slog.Info("local model stopped", "pid", 0, "mode", "noop")
		return nil
	}

	// Send graceful termination signal.
	proc.Kill()

	// Wait for process to exit or timeout.
	select {
	case <-m.exitCh:
		m.setState(routing.StateStopped)
		slog.Info("local model stopped", "pid", proc.Pid(), "mode", "graceful")
		return nil
	case <-time.After(m.cfg.ShutdownTimeout):
		// Process did not exit gracefully — force-kill.
		// Send a kill to the exit watcher so it unblocks, then
		// close the channel directly if needed.
		m.forceKill(proc)
		m.setState(routing.StateStopped)
		slog.Info("local model stopped", "pid", proc.Pid(), "mode", "forced")
		return nil
	case <-ctx.Done():
		m.forceKill(proc)
		m.setState(routing.StateStopped)
		slog.Info("local model stopped", "pid", proc.Pid(), "mode", "context-cancelled")
		return ctx.Err()
	}
}

// forceKill forcefully terminates the process and drains the exit channel.
func (m *Manager) forceKill(proc Process) {
	proc.Kill()
	// Wait briefly for the exit watcher to receive the notification.
	select {
	case <-m.exitCh:
	case <-time.After(500 * time.Millisecond):
	}
}

func (m *Manager) setState(state routing.LocalModelState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = state
	if state == routing.StateStopped || state == routing.StateFaulted {
		m.pid = 0
		m.proc = nil
	}
}

// StartIdleWatcher begins a background goroutine that stops the model
// after the configured idle timeout with no activity.
func (m *Manager) StartIdleWatcher() {
	go m.idleLoop()
}

// StopIdleWatcher stops the idle watcher goroutine.
func (m *Manager) StopIdleWatcher() {
	select {
	case m.idleStopCh <- struct{}{}:
	default:
	}
}

func (m *Manager) idleLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.idleStopCh:
			return
		case <-ticker.C:
			m.checkIdle()
		}
	}
}

func (m *Manager) checkIdle() {
	// Idle timeout of 0 means disabled
	if m.cfg.IdleTimeout <= 0 {
		return
	}

	state := m.State()
	if state != routing.StateReady {
		return
	}
	if m.IsBusy() {
		return
	}
	if m.modelIsProcessing() {
		m.UpdateActivity()
		return
	}

	last := m.LastActivity()
	if last.IsZero() {
		return
	}

	elapsed := time.Since(last)
	if elapsed >= m.cfg.IdleTimeout {
		slog.Info("model idle, stopping",
			"idle_duration", elapsed.String(),
			"timeout", m.cfg.IdleTimeout.String(),
		)
		if err := m.Stop(context.Background()); err != nil {
			slog.Error("failed to stop idle model", "err", err)
		}
	}
}
