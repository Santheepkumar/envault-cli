package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Santheepkumar/envault-cli/internal/config"
	"github.com/Santheepkumar/envault-cli/internal/credentials"
	vcrypto "github.com/Santheepkumar/envault-cli/internal/crypto"
	"github.com/Santheepkumar/envault-cli/internal/vault"
	"github.com/spf13/cobra"
)

const projectOne = "7b2d19cc-933b-4ded-a65e-4b17fecc720b"
const projectTwo = "8b2d19cc-933b-4ded-a65e-4b17fecc720b"

func setupOnboarding(t *testing.T) commandDeps {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	t.Setenv("ENVAULT_MACHINE", "")
	return commandDeps{
		getToken:     func(string) (string, error) { return "test-token", nil },
		setToken:     func(string, string) error { return nil },
		deleteToken:  func(string) error { return nil },
		terminal:     func(io.Reader) bool { return true },
		openBrowser:  func(string) error { return nil },
		pollInterval: time.Millisecond,
	}
}

func executeOnboarding(deps commandDeps, input string, args ...string) (string, error) {
	cmd := newCommand("test", deps)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestOriginPrecedenceAndInstallationIsolation(t *testing.T) {
	deps := setupOnboarding(t)
	check := func(want string, args ...string) {
		t.Helper()
		called := ""
		deps.getToken = func(origin string) (string, error) { called = origin; return "", credentials.ErrNotFound }
		out, _ := executeOnboarding(deps, "", append([]string{"status"}, args...)...)
		if called != want || !strings.Contains(out, want) {
			t.Fatalf("resolved %q, wanted %q: %s", called, want, out)
		}
	}
	check(config.DefaultOrigin)
	if err := config.SaveUser("https://remembered.example"); err != nil {
		t.Fatal(err)
	}
	check("https://remembered.example")
	if err := config.Save(config.Project{APIURL: "https://linked.example", ProjectID: projectOne}); err != nil {
		t.Fatal(err)
	}
	check("https://linked.example")
	check("https://explicit.example", "--api-url", "https://explicit.example")
	if _, err := executeOnboarding(deps, "", "history", "--api-url", "https://different.example"); err == nil || !strings.Contains(err.Error(), "installation differs") {
		t.Fatalf("cross-installation request accepted: %v", err)
	}
	if err := os.WriteFile(".envault.json", []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := executeOnboarding(deps, "", "status"); err == nil {
		t.Fatal("malformed binding ignored")
	}
}

func TestLoginPersistenceAndFailures(t *testing.T) {
	for _, mode := range []string{"success", "browser failure", "no browser", "headless", "foreign URL", "keychain failure", "denied", "save failure", "cancelled", "shared keychain entry"} {
		t.Run(mode, func(t *testing.T) {
			deps := setupOnboarding(t)
			if err := config.SaveUser("https://previous.example"); err != nil {
				t.Fatal(err)
			}
			stored, opened, revoked := false, false, false
			deps.setToken = func(origin, token string) error {
				if mode == "keychain failure" {
					return errors.New("keychain unavailable")
				}
				if token != "approved-token" {
					t.Errorf("unexpected token %q", token)
				}
				stored = true
				return nil
			}
			deps.openBrowser = func(string) error {
				opened = true
				if mode == "browser failure" {
					return errors.New("no browser")
				}
				return nil
			}
			if mode == "headless" {
				deps.terminal = func(io.Reader) bool { return false }
			}
			if mode == "shared keychain entry" {
				deps.getToken = func(string) (string, error) { return "approved-token", nil }
				deps.deleteToken = func(string) error { t.Error("shared new keychain entry deleted"); return nil }
			}
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/devices":
					uri := server.URL + "/?device=CODE"
					if mode == "shared keychain entry" {
						uri = strings.Replace(uri, "127.0.0.1", "localhost", 1)
					}
					if mode == "foreign URL" {
						uri = "https://evil.example/?device=CODE"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"deviceCode": "device-secret", "userCode": "CODE", "verificationUri": uri})
				case "/api/v1/devices/token":
					if mode == "denied" {
						w.WriteHeader(400)
						fmt.Fprint(w, `{"error":{"code":"INVALID_DEVICE","message":"expired"}}`)
						return
					}
					if mode == "save failure" {
						dir, _ := os.UserConfigDir()
						path := filepath.Join(dir, "envault", "config.json")
						if err := os.Remove(path); err != nil {
							t.Error(err)
						}
						if err := os.Mkdir(path, 0700); err != nil {
							t.Error(err)
						}
					}
					fmt.Fprint(w, `{"status":"approved","token":"approved-token"}`)
				case "/api/v1/logout":
					revoked = true
					fmt.Fprint(w, `{}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			args := []string{"login", "--api-url", server.URL}
			wantOrigin := server.URL
			if mode == "shared keychain entry" {
				args[2] = strings.Replace(server.URL, "127.0.0.1", "LOCALHOST", 1)
				wantOrigin = strings.ToLower(args[2])
			}
			if mode == "no browser" {
				args = append(args, "--no-browser")
			}
			cmd := newCommand("test", deps)
			var output bytes.Buffer
			cmd.SetIn(strings.NewReader(""))
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(args)
			if mode == "cancelled" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				cmd.SetContext(ctx)
			}
			err := cmd.Execute()
			success := mode == "success" || mode == "browser failure" || mode == "no browser" || mode == "headless" || mode == "shared keychain entry"
			if success != (err == nil) {
				t.Fatalf("err=%v output=%s", err, output.String())
			}
			if success {
				c, err := config.LoadUser()
				if err != nil || c.APIURL != wantOrigin || !stored || !strings.Contains(output.String(), "Run envault init") {
					t.Fatalf("login not persisted: %+v %v %s", c, err, output.String())
				}
			} else if mode != "save failure" {
				c, err := config.LoadUser()
				if err != nil || c.APIURL != "https://previous.example" {
					t.Fatalf("failed login changed origin: %+v %v", c, err)
				}
			}
			if mode == "foreign URL" && (opened || stored) {
				t.Fatal("foreign URL opened or token stored")
			}
			if (mode == "no browser" || mode == "headless") && opened {
				t.Fatal("browser opened unexpectedly")
			}
			if mode == "keychain failure" && !revoked {
				t.Fatal("unpersisted token not revoked")
			}
			if mode == "save failure" && (!stored || !strings.Contains(err.Error(), "could not remember")) {
				t.Fatalf("persistence failure unclear: %v", err)
			}
		})
	}
}

func TestProjectPickerAndRelinking(t *testing.T) {
	deps := setupOnboarding(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing authentication")
		}
		switch r.URL.Path {
		case "/api/v1/projects":
			if r.URL.Query().Get("before") == "" {
				_ = json.NewEncoder(w).Encode(map[string]any{"projects": []projectChoice{{projectOne, "same name"}}, "nextCursor": projectOne})
			} else {
				if r.URL.Query().Get("before") != projectOne {
					t.Error("wrong cursor")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"projects": []projectChoice{{projectOne, "same name"}, {projectTwo, "same name"}}})
			}
		case "/api/v1/projects/" + projectOne:
			_ = json.NewEncoder(w).Encode(map[string]any{"project": projectChoice{projectOne, "same name"}})
		case "/api/v1/projects/" + projectTwo:
			_ = json.NewEncoder(w).Encode(map[string]any{"project": projectChoice{projectTwo, "same name"}})
		default:
			t.Errorf("unexpected request %s (must not unlock vault)", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	if err := config.SaveUser(server.URL); err != nil {
		t.Fatal(err)
	}
	out, err := executeOnboarding(deps, "invalid\nn\n2\n", "init")
	if err != nil {
		t.Fatalf("picker failed: %v %s", err, out)
	}
	if c, err := config.Load(); err != nil || c.ProjectID != projectTwo || c.APIURL != server.URL {
		t.Fatalf("wrong binding %+v %v", c, err)
	}
	data, _ := os.ReadFile(".envault.json")
	before := requests
	if _, err := executeOnboarding(deps, "", "init"); err != nil {
		t.Fatal(err)
	}
	if requests != before {
		t.Fatal("idempotent init made network request")
	}
	if got, _ := os.ReadFile(".envault.json"); !bytes.Equal(data, got) {
		t.Fatal("idempotent init rewrote binding")
	}
	if _, err := executeOnboarding(deps, "n\n", "init", "--project", projectOne); err == nil {
		t.Fatal("relinked after cancellation")
	}
	deps.terminal = func(io.Reader) bool { return false }
	if _, err := executeOnboarding(deps, "", "init", "--project", projectOne); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("noninteractive replacement accepted: %v", err)
	}
	if _, err := executeOnboarding(deps, "", "init", "--project", projectOne, "--force"); err != nil {
		t.Fatal(err)
	}
	if c, _ := config.Load(); c.ProjectID != projectOne {
		t.Fatal("force did not relink")
	}
	before = requests
	if _, err := executeOnboarding(deps, "", "init", "--name", "new-project"); err == nil || requests != before {
		t.Fatal("remote project creation attempted before checking existing binding", err)
	}
	deps.terminal = func(io.Reader) bool { return true }
	if _, err := executeOnboarding(deps, "n\n2\n", "init", "--force"); err != nil {
		t.Fatal("forced project picker failed", err)
	}
	if c, _ := config.Load(); c.ProjectID != projectTwo {
		t.Fatal("forced picker did not relink")
	}
}

func TestInitNoninteractiveAndExplicitValidation(t *testing.T) {
	deps := setupOnboarding(t)
	deps.terminal = func(io.Reader) bool { return false }
	for _, args := range [][]string{{"init"}, {"init", "--project", projectOne, "--name", "new"}, {"init", "--project", "invalid"}} {
		if _, err := executeOnboarding(deps, "", args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error":{"code":"FORBIDDEN","message":"No access"}}`)
	}))
	defer server.Close()
	if _, err := executeOnboarding(deps, "", "init", "--project", projectOne, "--api-url", server.URL); err == nil {
		t.Fatal("inaccessible project linked")
	}
	if _, err := os.Stat(".envault.json"); !os.IsNotExist(err) {
		t.Fatal("failed init wrote configuration")
	}
}

func TestEmptyProjectPickerAndCancellation(t *testing.T) {
	deps := setupOnboarding(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"projects":[],"nextCursor":null}`) }))
	defer server.Close()
	o := &options{origin: server.URL, deps: deps}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var output bytes.Buffer
	cmd.SetErr(&output)
	id, name, err := chooseProject(cmd, o, bufio.NewReader(strings.NewReader("c\nmy-project\n")))
	if err != nil || id != "" || name != "my-project" {
		t.Fatalf("empty list creation: %q %q %v", id, name, err)
	}
	for _, input := range []string{"q\n", "", "c\n\n"} {
		if _, _, err := chooseProject(cmd, o, bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Fatalf("accepted cancellation/invalid name %q", input)
		}
	}
}

func TestStatusAndLogout(t *testing.T) {
	deps := setupOnboarding(t)
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/logout" {
			fmt.Fprint(w, `{}`)
			return
		}
		if r.URL.Path != "/api/v1/account" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		w.WriteHeader(status)
		if status != 200 {
			fmt.Fprint(w, `{"error":{"code":"UNAUTHORIZED","message":"Expired"}}`)
		} else {
			fmt.Fprint(w, `{"user":{"id":"user"}}`)
		}
	}))
	if err := config.SaveUser(server.URL); err != nil {
		t.Fatal(err)
	}
	if out, err := executeOnboarding(deps, "", "status"); err != nil || !strings.Contains(out, "Authentication: authenticated") {
		t.Fatalf("%s %v", out, err)
	}
	status = 401
	if out, err := executeOnboarding(deps, "", "status"); err == nil || !strings.Contains(out, "expired or revoked") || !strings.Contains(err.Error(), "login --api-url "+server.URL) {
		t.Fatalf("%s %v", out, err)
	}
	deleted := ""
	deps.deleteToken = func(origin string) error { deleted = origin; return nil }
	if _, err := executeOnboarding(deps, "", "logout"); err != nil || deleted != server.URL {
		t.Fatalf("logout %q %v", deleted, err)
	}
	if c, _ := config.LoadUser(); c.APIURL != server.URL {
		t.Fatal("logout forgot installation")
	}
	server.Close()
	if out, err := executeOnboarding(deps, "", "status"); err == nil || !strings.Contains(out, "unreachable") {
		t.Fatalf("%s %v", out, err)
	}
	deps.getToken = func(string) (string, error) { return "", credentials.ErrNotFound }
	if out, err := executeOnboarding(deps, "", "status"); err == nil || !strings.Contains(out, "not logged in") {
		t.Fatalf("%s %v", out, err)
	}
}

func TestLegacyKeychainOriginAndMachineRouting(t *testing.T) {
	deps := setupOnboarding(t)
	lookups := []string{}
	deps.getToken = func(origin string) (string, error) {
		lookups = append(lookups, origin)
		if origin == "https://VAULT.example:443" {
			return "legacy-token", nil
		}
		return "", credentials.ErrNotFound
	}
	o := &options{origin: "https://vault.example", legacyOrigin: "https://VAULT.example:443", deps: deps}
	a, err := humanAPI(o)
	if err != nil || a.Token != "legacy-token" || len(lookups) != 2 {
		t.Fatalf("legacy lookup: %v %v", lookups, err)
	}
	bundle := vault.MachineBundle{V: 1, APIURL: "https://machine.example", Token: "machine-token", PrivateKey: vcrypto.B64(make([]byte, 32)), ProjectID: projectOne, Environment: "prod", ID: "machine-id"}
	raw, _ := json.Marshal(bundle)
	t.Setenv("ENVAULT_MACHINE", string(raw))
	if err := config.SaveUser("https://unrelated.example"); err != nil {
		t.Fatal(err)
	}
	out, err := executeOnboarding(deps, "", "status")
	if err != nil || !strings.Contains(out, "Installation: https://machine.example") {
		t.Fatalf("%s %v", out, err)
	}
	for _, args := range [][]string{{"status", "--project", projectTwo}, {"status", "--env", "dev"}, {"status", "--api-url", "https://other.example"}} {
		if _, err := executeOnboarding(deps, "", args...); err == nil {
			t.Fatalf("machine override accepted %v", args)
		}
	}
	c, err := unlocked(context.Background(), &options{deps: deps, environment: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.API.BaseURL != bundle.APIURL || c.ProjectID != projectOne || c.Environment != "prod" {
		t.Fatalf("machine defaults overridden: %+v", c)
	}
}

func TestLegacyCredentialsMigrateAndLogoutClearsBothAliases(t *testing.T) {
	deps := setupOnboarding(t)
	tokens := map[string]string{}
	deps.getToken = func(origin string) (string, error) {
		if token, ok := tokens[origin]; ok {
			return token, nil
		}
		return "", credentials.ErrNotFound
	}
	deps.setToken = func(origin, token string) error { tokens[origin] = token; return nil }
	deps.deleteToken = func(origin string) error { delete(tokens, origin); return nil }
	revoked := map[string]bool{}
	origin := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/devices":
			_ = json.NewEncoder(w).Encode(map[string]any{"verificationUri": origin + "/?device=CODE", "deviceCode": "secret", "userCode": "CODE"})
		case "/api/v1/devices/token":
			fmt.Fprint(w, `{"status":"approved","token":"new-token"}`)
		case "/api/v1/account":
			fmt.Fprint(w, `{}`)
		case "/api/v1/logout":
			revoked[r.Header.Get("Authorization")] = true
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	origin = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	legacy := strings.Replace(origin, "localhost", "LOCALHOST", 1)
	tokens[legacy] = "old-token"
	data, _ := json.Marshal(config.Project{APIURL: legacy, ProjectID: projectOne})
	if err := os.WriteFile(".envault.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := executeOnboarding(deps, "", "status"); err != nil {
		t.Fatal(err)
	}
	if tokens[origin] != "old-token" || tokens[legacy] != "" {
		t.Fatalf("credential not migrated: %v", tokens)
	}
	// Simulate an old spelling still present when a new login replaces the canonical token.
	tokens[legacy] = "older-token"
	if _, err := executeOnboarding(deps, "", "login", "--no-browser"); err != nil {
		t.Fatal(err)
	}
	if _, err := executeOnboarding(deps, "", "logout"); err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 || !revoked["Bearer new-token"] || !revoked["Bearer older-token"] {
		t.Fatalf("credentials survived logout: %v revoked=%v", tokens, revoked)
	}
	if _, err := executeOnboarding(deps, "", "status"); !errors.Is(err, credentials.ErrNotFound) {
		t.Fatalf("logged out session reappeared: %v", err)
	}
}

func TestProjectCreationUnlocksLocallyAndSavesBinding(t *testing.T) {
	raw, err := os.ReadFile("../../protocol/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		UserID     string          `json:"userId"`
		Password   string          `json:"password"`
		PrivateKey string          `json:"privateKey"`
		Account    vcrypto.Account `json:"account"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	session, err := vcrypto.Unlock(fixture.UserID, fixture.Account, fixture.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer vcrypto.Clear(session.PrivateKey)
	defer vcrypto.Clear(session.VaultKey)
	for _, mode := range []string{"interactive", "script", "wrong password"} {
		t.Run(mode, func(t *testing.T) {
			deps := setupOnboarding(t)
			created := ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/account":
					_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"id": fixture.UserID}, "account": fixture.Account, "revision": 1})
				case r.URL.Path == "/api/v1/projects" && r.Method == "GET":
					fmt.Fprint(w, `{"projects":[]}`)
				case r.URL.Path == "/api/v1/projects" && r.Method == "POST":
					var project struct {
						ID    string                     `json:"id"`
						Name  string                     `json:"name"`
						Wraps map[string]vcrypto.KeyWrap `json:"wraps"`
					}
					if err := json.NewDecoder(r.Body).Decode(&project); err != nil {
						t.Error(err)
					}
					if project.Name != "new-project" || len(project.Wraps) != 3 {
						t.Errorf("invalid project creation: %+v", project)
					}
					for env, wrap := range project.Wraps {
						key, err := vcrypto.UnwrapKey(wrap, session.PrivateKey, vcrypto.Context{ProjectID: project.ID, Environment: env, Epoch: 1}, fixture.UserID)
						if err != nil || len(key) != 32 {
							t.Errorf("invalid local key wrap: %v", err)
						}
						vcrypto.Clear(key)
					}
					created = project.ID
					fmt.Fprint(w, `{}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			}))
			defer server.Close()
			args := []string{"init", "--api-url", server.URL, "--password-stdin"}
			input := fixture.Password + "\n"
			if mode == "interactive" {
				input = "c\nnew-project\n" + input
			} else {
				args = append(args, "--name", "new-project")
				deps.terminal = func(io.Reader) bool { return false }
			}
			if mode == "wrong password" {
				input = "wrong-password\n"
			}
			out, err := executeOnboarding(deps, input, args...)
			if mode == "wrong password" {
				if err == nil || created != "" {
					t.Fatal("wrong password created project")
				}
				if _, err := os.Stat(".envault.json"); !os.IsNotExist(err) {
					t.Fatal("failed creation saved binding")
				}
				return
			}
			if err != nil || created == "" {
				t.Fatalf("creation failed: %s %v", out, err)
			}
			c, err := config.Load()
			if err != nil || c.ProjectID != created || c.APIURL != server.URL {
				t.Fatalf("binding not saved: %+v %v", c, err)
			}
		})
	}
}
