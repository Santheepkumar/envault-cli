//go:build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"
)

func startTree(cmd *exec.Cmd) (processTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return processTree{}, err
	}
	return processTree{
		interrupt: func(s os.Signal) {
			sig, ok := s.(syscall.Signal)
			if !ok {
				sig = syscall.SIGTERM
			}
			_ = syscall.Kill(-cmd.Process.Pid, sig)
		},
		kill:  func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) },
		close: func() {},
		alive: func() bool { return syscall.Kill(-cmd.Process.Pid, 0) != syscall.ESRCH },
	}, nil
}

func exitCode(exit *exec.ExitError) int {
	if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return exit.ExitCode()
}
func terminationSignals() []os.Signal { return []os.Signal{syscall.SIGINT, syscall.SIGTERM} }
func environmentKey(k string) string  { return k }
