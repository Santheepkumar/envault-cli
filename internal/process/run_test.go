package process

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestRunHelper(t *testing.T) {
	mode := os.Getenv("ENVAULT_RUN_TEST")
	if mode == "" {
		return
	}
	switch mode {
	case "exit":
		os.Exit(23)
	case "args":
		if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "space ; $literal" || os.Getenv("SYNC_VALUE") != "line1\nline2" || os.Getenv("ENVAULT_MACHINE") != "" {
			os.Exit(24)
		}
		os.Exit(0)
	case "tree":
		cmd := exec.Command(os.Args[0], "-test.run=^TestRunHelper$")
		cmd.Env = append(os.Environ(), "ENVAULT_RUN_TEST=child")
		if err := cmd.Start(); err != nil {
			os.Exit(25)
		}
		_ = os.WriteFile(os.Getenv("ENVAULT_PID_FILE"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600)
		ignoreTermination()
		_ = os.WriteFile(os.Getenv("ENVAULT_READY_FILE"), []byte("ready"), 0600)
		for {
			time.Sleep(time.Hour)
		}
	case "grace-root", "grace-normal-root":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, terminationSignals()...)
		cmd := exec.Command(os.Args[0], "-test.run=^TestRunHelper$")
		cmd.Env = append(os.Environ(), "ENVAULT_RUN_TEST=grace-child")
		if err := cmd.Start(); err != nil {
			os.Exit(25)
		}
		for {
			if _, err := os.Stat(os.Getenv("ENVAULT_CHILD_READY")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = os.WriteFile(os.Getenv("ENVAULT_READY_FILE"), []byte("ready"), 0600)
		if mode == "grace-root" {
			<-signals
		}
		os.Exit(0)
	case "grace-child":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, terminationSignals()...)
		_ = os.WriteFile(os.Getenv("ENVAULT_CHILD_READY"), []byte("ready"), 0600)
		<-signals
		time.Sleep(200 * time.Millisecond)
		_ = os.WriteFile(os.Getenv("ENVAULT_CLEANED_FILE"), []byte("cleaned"), 0600)
		os.Exit(0)
	case "child":
		for {
			time.Sleep(time.Hour)
		}
	}
}

func TestRunPreservesArgumentsEnvironmentAndExit(t *testing.T) {
	t.Setenv("ENVAULT_MACHINE", "must-not-reach-child")
	err := Run(context.Background(), []string{os.Args[0], "-test.run=^TestRunHelper$", "--", "space ; $literal"}, map[string]string{"ENVAULT_RUN_TEST": "args", "SYNC_VALUE": "line1\nline2"})
	if err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), []string{os.Args[0], "-test.run=^TestRunHelper$"}, map[string]string{"ENVAULT_RUN_TEST": "exit"})
	if got := ExitCode(err); got != 23 {
		t.Fatalf("exit = %d, want 23: %v", got, err)
	}
}

func TestCancellationKillsProcessTree(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "pid")
	ready := filepath.Join(dir, "ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- run(ctx, []string{os.Args[0], "-test.run=^TestRunHelper$"}, map[string]string{"ENVAULT_RUN_TEST": "tree", "ENVAULT_PID_FILE": pidfile, "ENVAULT_READY_FILE": ready}, 150*time.Millisecond, nil)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, e := os.Stat(ready); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(data))
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancellation succeeded silently")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("termination exceeded grace period")
	}
	if !processStopped(pid, 3*time.Second) {
		t.Fatalf("descendant %d survived cancellation", pid)
	}
}

func TestDescendantsGetGraceAfterRootExits(t *testing.T) {
	for _, mode := range []string{"grace-root", "grace-normal-root"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			ready := filepath.Join(dir, "ready")
			cleaned := filepath.Join(dir, "cleaned")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- run(ctx, []string{os.Args[0], "-test.run=^TestRunHelper$"}, map[string]string{
					"ENVAULT_RUN_TEST": mode, "ENVAULT_READY_FILE": ready,
					"ENVAULT_CHILD_READY": filepath.Join(dir, "child-ready"), "ENVAULT_CLEANED_FILE": cleaned,
				}, time.Second, nil)
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("root not ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if mode == "grace-root" {
				cancel()
			}
			select {
			case <-result:
			case <-time.After(3 * time.Second):
				t.Fatal("termination exceeded grace")
			}
			if _, err := os.Stat(cleaned); err != nil {
				t.Fatal("descendant killed before graceful cleanup")
			}
		})
	}
}
