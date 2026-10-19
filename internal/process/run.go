package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"time"
)

const shutdownGrace = 5 * time.Second

type processTree struct {
	interrupt func(os.Signal)
	kill      func()
	close     func()
	alive     func() bool
}

// ExitCode preserves child exit status, including 128+signal on POSIX.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exitCode(exit)
	}
	return 1
}

func Run(ctx context.Context, args []string, values map[string]string) error {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, terminationSignals()...)
	defer signal.Stop(signals)
	return run(ctx, args, values, shutdownGrace, signals)
}

func run(ctx context.Context, args []string, values map[string]string, grace time.Duration, signals <-chan os.Signal) error {
	if len(args) == 0 {
		return errors.New("provide a command after --")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	merged := map[string]string{}
	for _, s := range os.Environ() {
		k, v, ok := strings.Cut(s, "=")
		if ok && !strings.EqualFold(k, "ENVAULT_MACHINE") {
			merged[environmentKey(k)] = v
		}
	}
	seen := map[string]bool{}
	for k, v := range values {
		normalized := environmentKey(k)
		if seen[normalized] {
			return fmt.Errorf("environment contains duplicate platform variable %q", k)
		}
		seen[normalized] = true
		merged[normalized] = v
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+merged[k])
	}
	tree, err := startTree(cmd)
	if err != nil {
		return err
	}
	defer tree.close()
	// Background descendants belong to this invocation even if the root exits first.
	defer tree.kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var timer *time.Timer
	var deadline <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	ctxDone := ctx.Done()
	var rootErr error
	forced := false
	var poll *time.Ticker
	var pollTree <-chan time.Time
	defer func() {
		if poll != nil {
			poll.Stop()
		}
	}()
	result := func() error {
		if rootErr == nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return rootErr
	}
	for {
		select {
		case err := <-done:
			rootErr = err
			done = nil
			if forced || !tree.alive() {
				return result()
			}
			// A launcher may exit before its workers finish shutting down.
			if timer == nil {
				tree.interrupt(nil)
				timer = time.NewTimer(grace)
				deadline = timer.C
			}
			poll = time.NewTicker(20 * time.Millisecond)
			pollTree = poll.C
		case <-pollTree:
			if !tree.alive() {
				return result()
			}
		case <-ctxDone:
			ctxDone = nil
			tree.interrupt(nil)
			if timer == nil {
				timer = time.NewTimer(grace)
				deadline = timer.C
			}
		case s := <-signals:
			tree.interrupt(s)
			if timer == nil {
				timer = time.NewTimer(grace)
				deadline = timer.C
			}
		case <-deadline:
			tree.kill()
			forced = true
			if done == nil {
				return result()
			}
			deadline = nil
		}
	}
}
