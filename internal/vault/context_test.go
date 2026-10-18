package vault

import (
	"context"
	"encoding/json"
	"github.com/envault/cli/internal/api"
	vcrypto "github.com/envault/cli/internal/crypto"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRejectSubstitutedEnvironmentAndVersion(t *testing.T) {
	for _, kind := range []string{"environment", "version", "revision"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if kind == "environment" {
					_ = json.NewEncoder(w).Encode(Environment{Context: vcrypto.Context{ProjectID: "expected", Environment: "prod", Epoch: 1}})
				} else {
					v := Version{Context: vcrypto.Context{ProjectID: "expected", Environment: "dev", Epoch: 1}, RevisionID: "other"}
					if kind == "version" {
						v.Environment = "prod"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"version": v})
				}
			}))
			defer server.Close()
			a, e := api.New(server.URL, "")
			if e != nil {
				t.Fatal(e)
			}
			c := &Client{API: a, ProjectID: "expected", Environment: "dev"}
			if kind == "environment" {
				if _, _, e = c.GetEnvironment(context.Background(), 0); e == nil {
					t.Fatal("context substitution accepted")
				}
			} else {
				if _, _, e = c.Read(context.Background(), "requested-revision"); e == nil {
					t.Fatal("version substitution accepted")
				}
			}
		})
	}
}
func TestPushUsesLocalBaseInsteadOfLatestRemoteHead(t *testing.T) {
	private := vcrypto.RandomKey()
	pub, _ := vcrypto.PublicKey(private)
	ctx := vcrypto.Context{ProjectID: "project", Environment: "dev", Epoch: 1}
	wrap, _ := vcrypto.WrapKey(vcrypto.RandomKey(), vcrypto.B64(pub), ctx, "user")
	latest := "version-two"
	base := "version-one"
	posted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(Environment{Context: ctx, Head: &latest, Wrap: wrap})
			return
		}
		posted = true
		var input struct {
			ExpectedHead *string `json:"expectedHead"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		if input.ExpectedHead == nil || *input.ExpectedHead != base {
			t.Error("local base was discarded")
		}
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"code":"CONFLICT","message":"pull before pushing"}}`))
	}))
	defer server.Close()
	a, _ := api.New(server.URL, "")
	c := &Client{API: a, PrivateKey: private, UserID: "user", ProjectID: "project", Environment: "dev", BaseHead: &base, BaseKnown: true}
	if _, e := c.Push(context.Background(), []byte("STALE=1")); e == nil {
		t.Fatal("stale edit accepted")
	}
	if !posted {
		t.Fatal("conditional push missing")
	}
	c.BaseKnown = false
	posted = false
	if _, e := c.Push(context.Background(), []byte("UNKNOWN=1")); e == nil || posted {
		t.Fatal("unknown local base silently uploaded")
	}
}
