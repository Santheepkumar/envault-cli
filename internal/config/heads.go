package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Local bases are scoped to the exact file and remote; unrelated pulls cannot bless a stale file.
func headPath(origin, project, env, file string) (string, error) {
	absolute, e := filepath.Abs(file)
	if e != nil {
		return "", e
	}
	dir, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256([]byte(origin + "\x00" + project + "\x00" + env + "\x00" + absolute))
	return filepath.Join(dir, "envault", "heads", fmt.Sprintf("%x.json", sum)), nil
}
func LoadHead(origin, project, env, file string) (*string, bool, error) {
	path, e := headPath(origin, project, env, file)
	if e != nil {
		return nil, false, e
	}
	data, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	var value struct {
		Head *string `json:"head"`
	}
	if e = json.Unmarshal(data, &value); e != nil {
		return nil, false, e
	}
	return value.Head, true, nil
}
func SaveHead(origin, project, env, file string, head *string) error {
	path, e := headPath(origin, project, env, file)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	data, e := json.Marshal(struct {
		Head *string `json:"head"`
	}{head})
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), "head-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
