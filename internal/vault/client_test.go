package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/envault/cli/internal/api"
	vcrypto "github.com/envault/cli/internal/crypto"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEncryptedFileRoundTripPreservesBytes(t *testing.T) {
	for name, raw := range map[string][]byte{
		"CRLF":              []byte("A=one\r\nB=two\r\n"),
		"BOM":               append([]byte{0xef, 0xbb, 0xbf}, []byte("A=one\n")...),
		"multiline":         []byte("PRIVATE='line one\nline two'\n"),
		"no-final-newline":  []byte("A=one"),
		"trailing-newlines": []byte("A=one\n\n\n"),
	} {
		t.Run(name, func(t *testing.T) {
			private := vcrypto.RandomKey()
			defer vcrypto.Clear(private)
			public, err := vcrypto.PublicKey(private)
			if err != nil {
				t.Fatal(err)
			}
			key := vcrypto.RandomKey()
			defer vcrypto.Clear(key)
			encryptionContext := vcrypto.Context{ProjectID: UUID(), Environment: "dev", Epoch: 1}
			user := UUID()
			wrap, err := vcrypto.WrapKey(key, vcrypto.B64(public), encryptionContext, user)
			if err != nil {
				t.Fatal(err)
			}
			var saved *Version
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "POST" {
					var input struct {
						RevisionID string      `json:"revisionId"`
						Box        vcrypto.Box `json:"box"`
					}
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					saved = &Version{Context: encryptionContext, RevisionID: input.RevisionID, Box: input.Box, Version: 1}
					_ = json.NewEncoder(w).Encode(map[string]any{"version": saved})
					return
				}
				if strings.Contains(r.URL.Path, "/versions/") {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": saved})
					return
				}
				_ = json.NewEncoder(w).Encode(Environment{Context: encryptionContext, Wrap: wrap})
			}))
			defer server.Close()
			apiClient, err := api.New(server.URL, "")
			if err != nil {
				t.Fatal(err)
			}
			client := &Client{API: apiClient, PrivateKey: private, UserID: user, ProjectID: encryptionContext.ProjectID, Environment: "dev"}
			dir := t.TempDir()
			source := filepath.Join(dir, "source.env")
			dest := filepath.Join(dir, "pulled.env")
			if err := os.WriteFile(source, raw, 0600); err != nil {
				t.Fatal(err)
			}
			input, err := ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Push(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			output, _, err := client.Read(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			if err = WriteFile(dest, output, false); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, raw) {
				t.Fatalf("bytes changed: got %q, want %q", got, raw)
			}
		})
	}
}

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
