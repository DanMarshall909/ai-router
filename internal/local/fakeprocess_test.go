package local_test

import (
	"context"
	"testing"
	"time"

	"github.com/DanMarshall909/ai-router/internal/local"
	"github.com/stretchr/testify/require"
)

func TestFakeProcess_StartSucceedsImmediately(t *testing.T) {
	fp := local.NewFakeProcess()
	err := fp.Start(context.Background())
	require.NoError(t, err, "because a default FakeProcess should start without error")
	require.NotZero(t, fp.Pid(), "because pid should be set after start")
}

func TestFakeProcess_StartWithDelay(t *testing.T) {
	fp := &local.FakeProcess{StartDelay: 50 * time.Millisecond}
	ctx := context.Background()
	start := time.Now()
	err := fp.Start(ctx)
	elapsed := time.Since(start)
	require.NoError(t, err, "because delayed start should still succeed")
	require.GreaterOrEqual(t, elapsed.Milliseconds(), int64(40), "because start should respect the configured delay")
}

func TestFakeProcess_StartReturnsError(t *testing.T) {
	fp := &local.FakeProcess{StartErr: context.DeadlineExceeded}
	err := fp.Start(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded, "because configured error should be returned")
}

func TestFakeProcess_WaitReturnsExitStatus(t *testing.T) {
	fp := &local.FakeProcess{
		ExitStatus: local.ExitStatus{Code: 1},
	}
	require.NoError(t, fp.Start(context.Background()))
	status := <-fp.Wait()
	require.Equal(t, 1, status.Code, "because configured exit code should be returned")
}

func TestFakeProcess_HangNeverExits(t *testing.T) {
	fp := &local.FakeProcess{ExitDelay: -1}
	require.NoError(t, fp.Start(context.Background()))
	ch := fp.Wait()
	select {
	case <-ch:
		t.Fatal("because a hanging process should not send on the Wait channel")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestFakeProcess_KillBlocksForever(t *testing.T) {
	fp := &local.FakeProcess{KillBlocks: true}
	require.NoError(t, fp.Start(context.Background()))
	done := make(chan error, 1)
	go func() { done <- fp.Kill() }()
	select {
	case <-done:
		t.Fatal("because KillBlocks should make Kill block forever")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestFakeProcess_KillReturnsConfiguredError(t *testing.T) {
	fp := &local.FakeProcess{KillErr: context.Canceled}
	require.NoError(t, fp.Start(context.Background()))
	err := fp.Kill()
	require.ErrorIs(t, err, context.Canceled, "because configured Kill error should be returned")
}

func TestFakeProcess_StdoutContent(t *testing.T) {
	fp := &local.FakeProcess{StdoutContent: "hello stdout"}
	require.NoError(t, fp.Start(context.Background()))
	buf := make([]byte, 1024)
	n, err := fp.Stdout().Read(buf)
	require.NoError(t, err, "because stdout should contain the configured content")
	require.Equal(t, "hello stdout", string(buf[:n]))
}

func TestFakeProcess_StderrContent(t *testing.T) {
	fp := &local.FakeProcess{StderrContent: "hello stderr"}
	require.NoError(t, fp.Start(context.Background()))
	buf := make([]byte, 1024)
	n, err := fp.Stderr().Read(buf)
	require.NoError(t, err, "because stderr should contain the configured content")
	require.Equal(t, "hello stderr", string(buf[:n]))
}

func TestFakeProcess_ContextCancellationStopsStartDelay(t *testing.T) {
	fp := &local.FakeProcess{StartDelay: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := fp.Start(ctx)
	require.Error(t, err, "because context cancellation should abort a delayed start")
}

func TestFakeProcess_PidZeroBeforeStart(t *testing.T) {
	fp := local.NewFakeProcess()
	require.Zero(t, fp.Pid(), "because pid should be zero before start")
}

func TestFakeProcess_ExitDelayZeroExitsImmediately(t *testing.T) {
	fp := &local.FakeProcess{ExitDelay: 0}
	require.NoError(t, fp.Start(context.Background()))
	select {
	case status := <-fp.Wait():
		require.Equal(t, 0, status.Code, "because default exit code should be zero")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("because zero exit delay should exit immediately")
	}
}
