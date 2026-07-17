package local

import (
	"context"
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

// Manager supervises the lifecycle of a llama-server process.
type Manager struct {
	mu    sync.Mutex
	state routing.LocalModelState
	pid   int
	proc  Process

	procFac    ProcessFactory
	cfg        config.LocalModelConfig
	healthURL  string
	httpClient *http.Client

	activeRequests atomic.Int32
	lastActivity   atomic.Int64

	sf singleflight.Group

	// exitCh receives the single exit notification from the watcher.
	exitCh chan ExitStatus
}

// NewManager creates a process manager from the given config and factory.
func NewManager(cfg config.LocalModelConfig, fac ProcessFactory) *Manager {
	return &Manager{
		state:      routing.StateStopped,
		procFac:    fac,
		cfg:        cfg,
		healthURL:  fmt.Sprintf("http://%s:%d/health", cfg.Host, cfg.Port),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		exitCh:     make(chan ExitStatus, 1),
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

func (m *Manager) doStart(ctx context.Context) (any, error) {
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
		return nil
	}

	// Send graceful termination signal.
	proc.Kill()

	// Wait for process to exit or timeout.
	select {
	case <-m.exitCh:
		m.setState(routing.StateStopped)
		return nil
	case <-time.After(m.cfg.ShutdownTimeout):
		// Process did not exit gracefully — force-kill.
		// Send a kill to the exit watcher so it unblocks, then
		// close the channel directly if needed.
		m.forceKill(proc)
		m.setState(routing.StateStopped)
		return nil
	case <-ctx.Done():
		m.forceKill(proc)
		m.setState(routing.StateStopped)
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
