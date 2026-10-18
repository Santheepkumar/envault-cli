//go:build windows

package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
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
		if ok && !strings.EqualFold(k, "ENVAULT_MACHINE") {
			merged[strings.ToUpper(k)] = v
		}
	}
	for k, v := range values {
		merged[strings.ToUpper(k)] = v
	}
	for k, v := range merged {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd.Run()
}
