package crypto

import (
	"encoding/json"
	"os"
	"testing"
)

type vector struct {
	UserID      string  `json:"userId"`
	Context     Context `json:"context"`
	RevisionID  string  `json:"revisionId"`
	PrivateKey  string  `json:"privateKey"`
	Key         string  `json:"key"`
	Plaintext   string  `json:"plaintext"`
	Box         Box     `json:"box"`
	Wrap        KeyWrap `json:"wrap"`
	Account     Account `json:"account"`
	Password    string  `json:"password"`
	RecoveryKey string  `json:"recoveryKey"`
}

func TestBrowserVectors(t *testing.T) {
	raw, e := os.ReadFile("../../protocol/vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var v vector
	if e = json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	private, _ := Decode(v.PrivateKey)
	key, e := UnwrapKey(v.Wrap, private, v.Context, v.UserID)
	if e != nil {
		t.Fatal(e)
	}
	if B64(key) != v.Key {
		t.Fatal("wrong wrapped key")
	}
	plaintext, e := Open(v.Box, key, SecretContext(v.Context, v.RevisionID))
	if e != nil || string(plaintext) != v.Plaintext {
		t.Fatalf("browser ciphertext: %v", e)
	}
	if _, e = UnwrapKey(v.Wrap, private, v.Context, "wrong-recipient"); e == nil {
		t.Fatal("recipient substitution accepted")
	}
	session, e := Unlock(v.UserID, v.Account, v.Password)
	if e != nil {
		t.Fatal(e)
	}
	defer Clear(session.PrivateKey)
	defer Clear(session.VaultKey)
	recovery, _ := Decode(v.RecoveryKey)
	updated, e := Recover(v.UserID, v.Account, recovery, "replacement-password")
	if e != nil {
		t.Fatal(e)
	}
	next, e := Unlock(v.UserID, updated, "replacement-password")
	if e != nil || B64(next.PrivateKey) != B64(session.PrivateKey) {
		t.Fatalf("recovery changed identity: %v", e)
	}
	if _, e = Unlock(v.UserID, v.Account, "wrong-password"); e == nil {
		t.Fatal("wrong password accepted")
	}
}
func TestMalformedEnvelopes(t *testing.T) {
	for _, b := range []Box{{}, {1, "AA==", "AA=="}, {99, "", ""}} {
		if _, e := Open(b, RandomKey(), "test"); e == nil {
			t.Fatal("invalid envelope accepted")
		}
	}
}
func TestGoRoundTrip(t *testing.T) {
	key := RandomKey()
	b, e := Seal([]byte("hello"), key, "bound context")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Open(b, key, "wrong context"); e == nil {
		t.Fatal("context substitution accepted")
	}
	p, e := Open(b, key, "bound context")
	if e != nil || string(p) != "hello" {
		t.Fatal(e)
	}
}
func FuzzOpen(f *testing.F) {
	f.Add("", "")
	f.Add("AA==", "AA==")
	f.Fuzz(func(t *testing.T, nonce, ct string) { _, _ = Open(Box{1, nonce, ct}, make([]byte, 32), "fuzz") })
}
