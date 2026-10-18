package vault

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSafeWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if e := WriteFile(path, []byte("A=1"), false); e != nil {
		t.Fatal(e)
	}
	if e := WriteFile(path, []byte("A=2"), false); e == nil {
		t.Fatal("silent overwrite")
	}
	if e := WriteFile(path, []byte("A=2"), true); e != nil {
		t.Fatal(e)
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("unsafe mode")
	}
	link := filepath.Join(dir, "link")
	if e := os.Symlink(path, link); e == nil {
		if e = WriteFile(link, []byte("leak"), true); e == nil {
			t.Fatal("symlink accepted")
		}
	}
}
