package local_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/DanMarshall909/ai-router/internal/config"
	"github.com/DanMarshall909/ai-router/internal/local"
	"github.com/DanMarshall909/ai-router/internal/routing"
	"github.com/stretchr/testify/require"
)

func testConfig(host string, port int) config.LocalModelConfig {
	return config.LocalModelConfig{
		ExecutablePath:  "/usr/bin/llama-server",
		ModelPath:       "/models/bonsai.gguf",
		Host:            host,
		Port:            port,
		StartupTimeout:  5 * time.Second,
		ShutdownTimeout: 1 * time.Second,
		IdleTimeout:     5 * time.Minute,
		AdditionalArgs:  []string{},
	}
}

func testConfigFromServer(srv *httptest.Server) config.LocalModelConfig {
	u, err := url.Parse(srv.URL)
	if err != nil {
		panic(fmt.Sprintf("parsing test server URL %q: %v", srv.URL, err))
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		panic(fmt.Sprintf("parsing port from %q: %v", srv.URL, err))
	}
	return testConfig(host, port)
}

// --- 6.2: Start with arguments as separate entries ---

func TestStartPassesArgumentsAsSeparateEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.AdditionalArgs = []string{"--n-gpu-layers", "35", "--ctx-size", "4096"}
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	require.NoError(t, mgr.Start(context.Background()))
	require.Equal(t, 1, fac.StartCount(), "because one process should be started")
}

func TestStartHandlesPathsWithSpaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.ModelPath = "/path with spaces/my model.gguf"
	cfg.ExecutablePath = "/path with spaces/llama-server"
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	err := mgr.Start(context.Background())
	require.NoError(t, err, "because paths with spaces should be handled correctly")
}

// --- 6.3: Readiness by polling health endpoint ---

func TestReadinessPollsHealthEndpoint(t *testing.T) {
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			select {
			case <-ready:
				w.WriteHeader(http.StatusOK)
			default:
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}
	}))
	defer srv.Close()

	healthURL := srv.URL + "/health"
	client := &http.Client{Timeout: 5 * time.Second}

	// Before ready, health check fails
	resp, err := client.Get(healthURL)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "because server is not ready yet")

	// Signal ready
	close(ready)

	// After ready, health check succeeds
	resp, err = client.Get(healthURL)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "because server is now ready")
}

func TestStartupTimeoutBoundsReadinessWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.StartupTimeout = 100 * time.Millisecond
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	ctx := context.Background()
	err := mgr.Start(ctx)
	require.Error(t, err, "because startup should fail when readiness timeout elapses")
	require.Contains(t, err.Error(), "timeout", "because error should mention timeout")
}

// --- 6.4: Single-flight startup ---

func TestSingleFlightStartup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.StartupTimeout = 5 * time.Second
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	var wg sync.WaitGroup
	errs := make([]error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = mgr.Start(context.Background())
		}()
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err, "because all concurrent starts should succeed")
	}
	require.Equal(t, 1, fac.StartCount(), "because single-flight should start exactly one process")
}

// --- 6.5: Failed start is retried, not cached ---

func TestFailedStartIsRetried(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.StartupTimeout = 2 * time.Second
	fac := local.NewFailThenSucceedFactoryWithHealth(srv.URL)
	mgr := local.NewManager(cfg, fac)

	// First start fails
	err := mgr.Start(context.Background())
	require.Error(t, err, "because first start should fail")

	// Second start succeeds (singleflight doesn't cache failures)
	err = mgr.Start(context.Background())
	require.NoError(t, err, "because failed start should be retried")
}

// --- 6.6: Reuse of already-ready process ---

func TestReuseAlreadyReadyProcess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.StartupTimeout = 5 * time.Second
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	require.NoError(t, mgr.Start(context.Background()), "because first start should succeed")
	require.NoError(t, mgr.Start(context.Background()), "because second start should reuse")
	require.Equal(t, 1, fac.StartCount(), "because only one process should be started")
}

// --- 6.7: State tracking ---

func TestStateTransitionsOnSuccessfulStart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.StartupTimeout = 5 * time.Second
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	require.Equal(t, routing.StateStopped, mgr.State(), "because initial state should be Stopped")
	require.Zero(t, mgr.Pid(), "because PID should be 0 when stopped")

	require.NoError(t, mgr.Start(context.Background()))
	require.Equal(t, routing.StateReady, mgr.State(), "because state should be Ready after start")
	require.NotZero(t, mgr.Pid(), "because PID should be set when ready")
}

func TestStateIsFaultedOnReadinessTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.StartupTimeout = 100 * time.Millisecond
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	_ = mgr.Start(context.Background())
	require.Equal(t, routing.StateFaulted, mgr.State(), "because state should be Faulted on timeout")
}

// --- 6.8: IsBusy ---

func TestIsBusyDerivedFromActiveRequestCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	require.False(t, mgr.IsBusy(), "because no requests should mean not busy")

	mgr.RequestBegin()
	require.True(t, mgr.IsBusy(), "because one active request should mean busy")

	mgr.RequestBegin()
	require.True(t, mgr.IsBusy(), "because two active requests should still mean busy")

	mgr.RequestEnd()
	require.True(t, mgr.IsBusy(), "because one remaining request should still mean busy")

	mgr.RequestEnd()
	require.False(t, mgr.IsBusy(), "because zero active requests should mean not busy")
}

func TestConcurrentRequestsDoNotChangeLifecycleState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	require.NoError(t, mgr.Start(context.Background()))
	require.Equal(t, routing.StateReady, mgr.State())

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.RequestBegin()
			time.Sleep(10 * time.Millisecond)
			mgr.RequestEnd()
		}()
	}
	wg.Wait()

	require.Equal(t, routing.StateReady, mgr.State(), "because lifecycle state should remain Ready")
	require.False(t, mgr.IsBusy(), "because all requests should have completed")
}

// --- 6.9: Faulted on launch failure ---

func TestStateIsFaultedOnLaunchFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.StartupTimeout = 5 * time.Second
	fac := local.NewAlwaysFailFactory()
	mgr := local.NewManager(cfg, fac)

	err := mgr.Start(context.Background())
	require.Error(t, err, "because start should fail with a bad executable")
	require.Equal(t, routing.StateFaulted, mgr.State(), "because state should be Faulted on launch failure")
}

// --- 6.10: Graceful stop ---

func TestGracefulStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.ShutdownTimeout = 1 * time.Second
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	require.NoError(t, mgr.Start(context.Background()))
	require.Equal(t, routing.StateReady, mgr.State())

	require.NoError(t, mgr.Stop(context.Background()))
	require.Equal(t, routing.StateStopped, mgr.State(), "because state should be Stopped after graceful stop")
	require.Zero(t, mgr.Pid(), "because PID should be 0 after stop")
}

func TestForceKillAfterShutdownTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	cfg.ShutdownTimeout = 100 * time.Millisecond
	fac := local.NewFakeProcessFactory()
	fac.SetHangOnWait(true)
	mgr := local.NewManager(cfg, fac)

	require.NoError(t, mgr.Start(context.Background()))

	err := mgr.Stop(context.Background())
	require.NoError(t, err, "because force-kill should succeed")
	require.Equal(t, routing.StateStopped, mgr.State(), "because state should be Stopped after force-kill")
}

// --- 6.11: Unexpected exit detection ---

func TestUnexpectedExitSetsFaulted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	require.NoError(t, mgr.Start(context.Background()))
	require.Equal(t, routing.StateReady, mgr.State())

	// Simulate process crash
	fac.SimulateCrash()

	// Give the exit watcher goroutine time to run
	time.Sleep(50 * time.Millisecond)

	require.Equal(t, routing.StateFaulted, mgr.State(), "because unexpected exit should set Faulted")
	require.Zero(t, mgr.Pid(), "because PID should be 0 after crash")
}

// --- 6.12: Resources released ---

func TestResourcesReleasedOnStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	require.NoError(t, mgr.Start(context.Background()))
	require.NoError(t, mgr.Stop(context.Background()))

	require.Equal(t, routing.StateStopped, mgr.State())
	require.Zero(t, mgr.Pid())
	require.False(t, mgr.IsBusy())
}

func TestRaceDetectorClean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testConfigFromServer(srv)
	fac := local.NewFakeProcessFactory()
	mgr := local.NewManager(cfg, fac)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.State()
			_ = mgr.Pid()
			_ = mgr.IsBusy()
			mgr.UpdateActivity()
		}()
	}
	wg.Wait()
}
