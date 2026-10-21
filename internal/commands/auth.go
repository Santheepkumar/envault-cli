package commands

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Santheepkumar/envault-cli/internal/api"
	"github.com/Santheepkumar/envault-cli/internal/credentials"
	vcrypto "github.com/Santheepkumar/envault-cli/internal/crypto"
	"github.com/spf13/cobra"
	"io"
	"os"
	"time"
)

func writeJSON(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
func authCommands(o *options) []*cobra.Command {
	login := &cobra.Command{Use: "login", Short: "Approve this CLI in your browser", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, e := api.New(o.origin, "")
		if e != nil {
			return e
		}
		verifier := vcrypto.B64(vcrypto.RandomKey())
		challenge := sha256.Sum256([]byte(verifier))
		var device struct {
			DeviceCode      string `json:"deviceCode"`
			UserCode        string `json:"userCode"`
			VerificationURI string `json:"verificationUri"`
		}
		if e = a.Do(cmd.Context(), "POST", "/devices", map[string]string{"challenge": fmt.Sprintf("%x", challenge)}, &device); e != nil {
			return e
		}
		fmt.Fprintf(os.Stderr, "Open %s\nConfirm code: %s\n", device.VerificationURI, device.UserCode)
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
		defer cancel()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				var response struct {
					Status string `json:"status"`
					Token  string `json:"token"`
				}
				if e = a.Do(ctx, "POST", "/devices/token", map[string]string{"deviceCode": device.DeviceCode, "verifier": verifier}, &response); e != nil {
					return e
				}
				if response.Status == "approved" {
					if e = credentials.Set(o.origin, response.Token); e != nil {
						a.Token = response.Token
						_ = a.Do(ctx, "POST", "/logout", map[string]any{}, nil)
						return e
					}
					fmt.Fprintln(os.Stdout, "CLI connected. Credentials expire in 30 days.")
					return nil
				}
			}
		}
	}}
	logout := &cobra.Command{Use: "logout", Short: "Revoke this CLI session and remove its keychain token", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		if e = a.Do(cmd.Context(), "POST", "/logout", map[string]any{}, nil); e != nil {
			return e
		}
		return credentials.Delete(o.origin)
	}}
	recover := &cobra.Command{Use: "recover", Short: "Recover your vault using a recovery kit", Args: cobra.NoArgs}
	var kit string
	recover.Flags().StringVar(&kit, "kit", "", "Recovery kit JSON file")
	_ = recover.MarkFlagRequired("kit")
	recover.RunE = func(cmd *cobra.Command, _ []string) error {
		file, e := os.Open(kit)
		if e != nil {
			return e
		}
		recovery, e := readRecoveryKit(file, o.origin)
		_ = file.Close()
		if e != nil {
			return e
		}
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		var result struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			Account  vcrypto.Account `json:"account"`
			Revision int             `json:"revision"`
		}
		if e = a.Do(cmd.Context(), "GET", "/account", nil, &result); e != nil {
			return e
		}
		if result.User.ID != recovery.UserID {
			return fmt.Errorf("recovery kit belongs to a different account")
		}
		pw, e := password(o, "New vault password (12+ characters)")
		if e != nil {
			return e
		}
		key, e := vcrypto.Decode(recovery.RecoveryKey)
		if e != nil {
			return e
		}
		defer vcrypto.Clear(key)
		updated, e := vcrypto.Recover(recovery.UserID, result.Account, key, pw)
		if e != nil {
			return e
		}
		return a.Do(cmd.Context(), "PUT", "/account", map[string]any{"account": updated, "expectedRevision": result.Revision, "recoveryConfirmed": true}, nil)
	}
	return []*cobra.Command{login, logout, recover}
}
