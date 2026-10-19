//go:build windows

package process

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"os/signal"
	"strings"
	"testing"
	"time"
)

func ignoreTermination() { signal.Ignore(os.Interrupt) }
func processStopped(pid int, timeout time.Duration) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, uint32(timeout.Milliseconds()))
	return err == nil && status == windows.WAIT_OBJECT_0
}

func TestRejectsCaseInsensitiveEnvironmentDuplicates(t *testing.T) {
	err := Run(context.Background(), []string{os.Args[0], "-test.run=^TestRunHelper$"}, map[string]string{"A": "one", "a": "two"})
	if err == nil || !strings.Contains(err.Error(), "duplicate platform variable") {
		t.Fatalf("duplicate accepted: %v", err)
	}
}
