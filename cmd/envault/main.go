package main

import (
	"errors"
	"fmt"
	"github.com/envault/cli/internal/commands"
	"github.com/envault/cli/internal/process"
	"os"
	"os/exec"
)

var version = "dev"

func main() {
	if e := commands.New(version).Execute(); e != nil {
		var child *exec.ExitError
		if errors.As(e, &child) {
			os.Exit(process.ExitCode(e))
		}
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
