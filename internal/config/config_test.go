package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	return dir
}

func TestNormalizeURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://VAULT.example:443/": "https://vault.example",
		"https://vault.example:8443": "https://vault.example:8443",
		"http://localhost:80/":       "http://localhost",
		"http://[::1]:3000":          "http://[::1]:3000",
	} {
		got, err := NormalizeURL(raw)
		if err != nil || got != want {
			t.Fatalf("NormalizeURL(%q)=%q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"http://vault.example", "https://user@vault.example", "https://vault.example/path", "https://vault.example?", "https://vault.example#fragment", "https://:443", "https://vault.example/%2f"} {
		if _, err := NormalizeURL(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestFindNearestProjectAndKeepWorkingDirectory(t *testing.T) {
	root := isolate(t)
	if err := Save(Project{APIURL: "https://vault.example", ProjectID: "outer"}); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "child")
	if err := os.MkdirAll(filepath.Join(child, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(child, "nested"))
	c, path, err := Find()
	if err != nil || c.ProjectID != "outer" || path != filepath.Join(root, ".envault.json") {
		t.Fatalf("%+v %s %v", c, path, err)
	}
	if err := os.WriteFile(".env", []byte("LOCAL=1"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(child, "nested", ".env")); err != nil {
		t.Fatal("working directory changed", err)
	}
	if err := SaveProject(filepath.Join(child, ".envault.json"), Project{APIURL: DefaultOrigin, ProjectID: "inner"}, false); err != nil {
		t.Fatal(err)
	}
	c, _, err = Find()
	if err != nil || c.ProjectID != "inner" {
		t.Fatalf("nearest project not used: %+v %v", c, err)
	}
	if err := os.WriteFile(filepath.Join(child, ".envault.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Find(); err == nil {
		t.Fatal("malformed nearest file silently ignored")
	}
}

func TestUserConfigurationAtomicAndPrivate(t *testing.T) {
	isolate(t)
	if c, err := LoadUser(); err != nil || c.APIURL != "" {
		t.Fatalf("missing user config: %+v %v", c, err)
	}
	if err := SaveUser("https://VAULT.example:443/"); err != nil {
		t.Fatal(err)
	}
	c, err := LoadUser()
	if err != nil || c.APIURL != "https://vault.example" {
		t.Fatalf("%+v %v", c, err)
	}
	path, _ := userPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 1 {
		t.Fatalf("unexpected fields: %s", data)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatalf("permissions %o", info.Mode().Perm())
		}
	}
	if err := SaveUser("http://evil.example"); err == nil {
		t.Fatal("invalid origin saved")
	}
	if c, _ := LoadUser(); c.APIURL != "https://vault.example" {
		t.Fatal("failed save changed config")
	}
	if err := SaveUser(DefaultOrigin); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUser(); err == nil {
		t.Fatal("invalid configuration ignored")
	}
}

func TestProjectReplacementAndSymlinkRefusal(t *testing.T) {
	isolate(t)
	c := Project{APIURL: DefaultOrigin, ProjectID: "first"}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	c.ProjectID = "second"
	if err := Save(c); err == nil {
		t.Fatal("existing config overwritten without replacement")
	}
	if err := SaveProject(".envault.json", c, true); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(); err != nil || got.ProjectID != "second" {
		t.Fatalf("%+v %v", got, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Symlink(".envault.json", "link.json"); err != nil {
		t.Fatal(err)
	}
	if err := SaveProject("link.json", c, true); err == nil {
		t.Fatal("symlink overwritten")
	}
}

func TestLegacyFingerprintPinsSurviveNormalization(t *testing.T) {
	isolate(t)
	legacy, origin := "https://VAULT.example:443", "https://vault.example"
	if err := Pin(legacy, "user-id", "trusted-fingerprint"); err != nil {
		t.Fatal(err)
	}
	if err := MigratePins(legacy, origin); err != nil {
		t.Fatal(err)
	}
	if err := Pin(origin, "user-id", "changed-fingerprint"); err == nil {
		t.Fatal("normalization bypassed trusted pin")
	}
	if err := MigratePins(legacy, "https://other.example"); err == nil {
		t.Fatal("migrated pins to another installation")
	}
	if err := Pin(legacy, "other-user", "old"); err != nil {
		t.Fatal(err)
	}
	if err := Pin(origin, "other-user", "new"); err != nil {
		t.Fatal(err)
	}
	if err := MigratePins(legacy, origin); err == nil {
		t.Fatal("conflicting pins silently merged")
	}
}
