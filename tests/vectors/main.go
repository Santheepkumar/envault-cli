// Regenerates the Go-produced half of the public cross-language fixtures.
package main

import (
	"encoding/json"
	vcrypto "github.com/Santheepkumar/envault-cli/internal/crypto"
	"os"
)

func main() {
	data, e := os.ReadFile("protocol/vectors.json")
	if e != nil {
		panic(e)
	}
	var v struct {
		UserID      string          `json:"userId"`
		Context     vcrypto.Context `json:"context"`
		RevisionID  string          `json:"revisionId"`
		PrivateKey  string          `json:"privateKey"`
		PublicKey   string          `json:"publicKey"`
		Key         string          `json:"key"`
		Plaintext   string          `json:"plaintext"`
		Account     vcrypto.Account `json:"account"`
		RecoveryKey string          `json:"recoveryKey"`
	}
	if e = json.Unmarshal(data, &v); e != nil {
		panic(e)
	}
	var out map[string]any
	if e = json.Unmarshal(data, &out); e != nil {
		panic(e)
	}
	key, _ := vcrypto.Decode(v.Key)
	out["goBox"], e = vcrypto.Seal([]byte(v.Plaintext), key, vcrypto.SecretContext(v.Context, v.RevisionID))
	if e != nil {
		panic(e)
	}
	out["goWrap"], e = vcrypto.WrapKey(key, v.PublicKey, v.Context, v.UserID)
	if e != nil {
		panic(e)
	}
	recovery, _ := vcrypto.Decode(v.RecoveryKey)
	out["goRecoveredAccount"], e = vcrypto.Recover(v.UserID, v.Account, recovery, "go-recovered-password")
	if e != nil {
		panic(e)
	}
	data, e = json.MarshalIndent(out, "", "  ")
	if e != nil {
		panic(e)
	}
	if e = os.WriteFile("protocol/vectors.json", append(data, '\n'), 0644); e != nil {
		panic(e)
	}
}
