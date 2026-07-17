package local

import (
	"context"
	"io"
	"os/exec"
)

// ExitStatus represents the result of a process exiting.
type ExitStatus struct {
	Code int
	Err  error
}

// Process abstracts a subprocess for testability.
type Process interface {
	// Start begins execution of the process.
	Start(ctx context.Context) error

	// Wait returns a channel that receives exactly one ExitStatus
	// when the process exits, then closes.
	Wait() <-chan ExitStatus

	// Kill forcefully terminates the process.
	Kill() error

	// Pid returns the OS process ID, or 0 if not started.
	Pid() int

	// Stdout returns a reader for the process's standard output.
	Stdout() io.ReadCloser

	// Stderr returns a reader for the process's standard error.
	Stderr() io.ReadCloser
}

// ProcessFactory creates Process instances.
type ProcessFactory interface {
	// NewProcess creates a new Process for the given command.
	NewProcess(name string, args ...string) Process
}

// execProcess is the real Process implementation wrapping os/exec.
type execProcess struct {
	name string
	args []string

	cmd        *exec.Cmd
	stdoutPipe io.ReadCloser
	stderrPipe io.ReadCloser
	waitCh     chan ExitStatus
}

// execFactory creates real execProcess instances.
type execFactory struct{}

// NewExecFactory returns a ProcessFactory that creates real OS processes.
func NewExecFactory() ProcessFactory {
	return &execFactory{}
}

func (f *execFactory) NewProcess(name string, args ...string) Process {
	return &execProcess{name: name, args: args}
}

func (p *execProcess) Start(ctx context.Context) error {
	p.cmd = exec.CommandContext(ctx, p.name, p.args...)

	var err error
	p.stdoutPipe, err = p.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	p.stderrPipe, err = p.cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := p.cmd.Start(); err != nil {
		return err
	}

	p.waitCh = make(chan ExitStatus, 1)
	go func() {
		err := p.cmd.Wait()
		code := 0
		if p.cmd.ProcessState != nil {
			code = p.cmd.ProcessState.ExitCode()
		}
		p.waitCh <- ExitStatus{Code: code, Err: err}
		close(p.waitCh)
	}()

	return nil
}

func (p *execProcess) Wait() <-chan ExitStatus {
	return p.waitCh
}

func (p *execProcess) Kill() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p *execProcess) Pid() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *execProcess) Stdout() io.ReadCloser {
	return p.stdoutPipe
}

func (p *execProcess) Stderr() io.ReadCloser {
	return p.stderrPipe
}
