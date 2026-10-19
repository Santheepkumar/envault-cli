//go:build !windows

package process

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func ignoreTermination() { signal.Ignore(syscall.SIGTERM) }
func processStopped(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
func TestSignalExitCode(t *testing.T) {
	err := Run(context.Background(), []string{"sh", "-c", "kill -TERM $$"}, nil)
	if got := ExitCode(err); got != 143 {
		t.Fatalf("exit=%d, want143: %v", got, err)
	}
}
func TestForwardedInterruptExitCode(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGINT
	err := run(context.Background(), []string{"sh", "-c", "sleep 30"}, nil, 150*time.Millisecond, signals)
	if got := ExitCode(err); got != 130 {
		t.Fatalf("exit=%d, want130: %v", got, err)
	}
}
