package commands

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecoveryKitValidation(t *testing.T) {
	valid := map[string]any{"v": 1, "apiUrl": "https://vault.example", "userId": "12345678-1234-4234-8234-123456789abc", "recoveryKey": base64.StdEncoding.EncodeToString(make([]byte, 32))}
	for _, test := range []struct {
		name, field string
		value       any
	}{
		{"wrong installation same account", "apiUrl", "https://other.example"},
		{"different port", "apiUrl", "https://vault.example:8443"},
		{"unsupported version", "v", 2},
		{"missing version", "v", nil},
		{"missing origin", "apiUrl", nil},
		{"origin has path", "apiUrl", "https://vault.example/private"},
		{"origin has userinfo", "apiUrl", "https://user@vault.example"},
		{"nonlocal http", "apiUrl", "http://vault.example"},
		{"invalid account", "userId", "not-a-uuid"},
		{"invalid key", "recoveryKey", "invalid"},
		{"short key", "recoveryKey", base64.StdEncoding.EncodeToString(make([]byte, 31))},
		{"unknown field", "unexpected", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := make(map[string]any)
			for key, value := range valid {
				input[key] = value
			}
			if test.value == nil {
				delete(input, test.field)
			} else {
				input[test.field] = test.value
			}
			data, _ := json.Marshal(input)
			if _, err := readRecoveryKit(strings.NewReader(string(data)), "https://vault.example"); err == nil {
				t.Fatal("invalid kit accepted")
			}
		})
	}
	data, _ := json.Marshal(valid)
	for _, origin := range []string{"https://vault.example", "https://VAULT.example:443/"} {
		kit, err := readRecoveryKit(strings.NewReader(string(data)), origin)
		if err != nil || kit.UserID != valid["userId"] || kit.RecoveryKey != valid["recoveryKey"] {
			t.Fatalf("valid same-origin account kit rejected: %v", err)
		}
	}
	for _, invalid := range []string{string(data) + "{}", "[{}]", "null", string(data) + strings.Repeat(" ", recoveryKitLimit)} {
		if _, err := readRecoveryKit(strings.NewReader(invalid), "https://vault.example"); err == nil {
			t.Fatal("invalid or oversized document accepted")
		}
	}
}

func TestRecoverRejectsWrongOriginBeforeKeychain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kit.json")
	data, _ := json.Marshal(recoveryKit{V: 1, APIURL: "https://other.example", UserID: "12345678-1234-4234-8234-123456789abc", RecoveryKey: base64.StdEncoding.EncodeToString(make([]byte, 32))})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	command := New("test")
	command.SetArgs([]string{"recover", "--api-url", "https://vault.example", "--kit", path})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "different installation") {
		t.Fatalf("kit must be rejected before keychain/network access: %v", err)
	}
}
