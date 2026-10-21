// Integration driver: exercises the public CLI's HTTP and vault layers against a running application.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Santheepkumar/envault-cli/internal/api"
	vcrypto "github.com/Santheepkumar/envault-cli/internal/crypto"
	"github.com/Santheepkumar/envault-cli/internal/vault"
	"os"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	var in struct {
		APIURL    string `json:"apiUrl"`
		Token     string `json:"token"`
		UserID    string `json:"userId"`
		ProjectID string `json:"projectId"`
		Password  string `json:"password"`
	}
	if e := json.NewDecoder(os.Stdin).Decode(&in); e != nil {
		return e
	}
	a, e := api.New(in.APIURL, in.Token)
	if e != nil {
		return e
	}
	var r struct {
		Account vcrypto.Account `json:"account"`
	}
	ctx := context.Background()
	if e = a.Do(ctx, "GET", "/account", nil, &r); e != nil {
		return e
	}
	s, e := vcrypto.Unlock(in.UserID, r.Account, in.Password)
	if e != nil {
		return e
	}
	c := &vault.Client{API: a, PrivateKey: s.PrivateKey, VaultKey: s.VaultKey, UserID: in.UserID, ProjectID: in.ProjectID, Environment: "dev"}
	defer c.Close()
	data, v, e := c.Read(ctx, "")
	if e != nil {
		return e
	}
	if v == nil || len(data) == 0 {
		return fmt.Errorf("browser version missing")
	}
	vcrypto.Clear(data)
	c.BaseKnown = true
	c.BaseHead = &v.RevisionID
	if _, e = c.Push(ctx, []byte("CLI_VALUE=from-go\n")); e != nil {
		return e
	}
	data, _, e = c.Read(ctx, "")
	if e != nil {
		return e
	}
	if string(data) != "CLI_VALUE=from-go\n" {
		return fmt.Errorf("roundtrip mismatch")
	}
	vcrypto.Clear(data)
	return nil
}
