//go:build !windows

package vault

import "os"

func protectedTemp(dir string) (*os.File, error) { return os.CreateTemp(dir, ".envault-*") }
func replaceFile(from, to string) error          { return os.Rename(from, to) }
