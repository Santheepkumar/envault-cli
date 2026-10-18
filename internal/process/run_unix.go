//go:build !windows

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"syscall"
)

func Run(ctx context.Context, args []string, values map[string]string) error {
	if len(args) == 0 {
		return errors.New("provide a command after --")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	merged := map[string]string{}
	for _, s := range os.Environ() {
		k, v, ok := strings.Cut(s, "=")
		if ok && k != "ENVAULT_MACHINE" {
			merged[k] = v
		}
	}
	for k, v := range values {
		merged[k] = v
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+merged[k])
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return nil
	}
	if e := cmd.Start(); e != nil {
		return e
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case s := <-signals:
				if sig, ok := s.(syscall.Signal); ok {
					_ = syscall.Kill(-cmd.Process.Pid, sig)
				}
			case <-done:
				return
			}
		}
	}()
	return cmd.Wait()
}
