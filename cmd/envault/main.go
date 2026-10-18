package main

import (
	"errors"
	"fmt"
	"github.com/envault/cli/internal/commands"
	"os"
	"os/exec"
)

var version = "dev"

func main() {
	if e := commands.New(version).Execute(); e != nil {
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
