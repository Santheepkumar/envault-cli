package commands

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Santheepkumar/envault-cli/internal/api"
	"github.com/Santheepkumar/envault-cli/internal/config"
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
	var noBrowser bool
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
		if e = validateVerificationURL(device.VerificationURI, o.origin); e != nil {
			return e
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Installation: %s\nOpen %s\nConfirm code: %s\n", o.origin, device.VerificationURI, device.UserCode)
		if !noBrowser && o.deps.terminal(cmd.InOrStdin()) {
			if e := o.deps.openBrowser(device.VerificationURI); e != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "Could not open your browser; open the URL above to continue.")
			}
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
		defer cancel()
		ticker := time.NewTicker(o.deps.pollInterval)
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
					if response.Token == "" {
						return fmt.Errorf("server approved login without a token")
					}
					if e = o.deps.setToken(o.origin, response.Token); e != nil {
						a.Token = response.Token
						_ = a.Do(ctx, "POST", "/logout", map[string]any{}, nil)
						return e
					}
					if o.legacyOrigin != o.origin {
						old, err := o.deps.getToken(o.legacyOrigin)
						if err != nil && !errors.Is(err, credentials.ErrNotFound) {
							return fmt.Errorf("CLI token stored, but could not read legacy credential for cleanup: %w", err)
						}
						// Case-insensitive keychains may resolve both spellings to the new entry.
						if err == nil && old != response.Token {
							a.Token = old
							if err := a.Do(ctx, "POST", "/logout", map[string]any{}, nil); err != nil {
								var apiErr *api.Error
								if !errors.As(err, &apiErr) || apiErr.Status != 401 {
									return fmt.Errorf("CLI token stored, but legacy session cleanup failed: %w", err)
								}
							}
							if err := o.deps.deleteToken(o.legacyOrigin); err != nil {
								return fmt.Errorf("CLI token stored, but could not remove legacy credential: %w", err)
							}
						}
					}
					if e = config.SaveUser(o.origin); e != nil {
						return fmt.Errorf("CLI connected and token stored, but could not remember installation; retry login with --api-url %s: %w", o.origin, e)
					}
					fmt.Fprintln(cmd.OutOrStdout(), "CLI connected. Credentials expire in 30 days.\nRun envault init to link a project.")
					return nil
				}
			}
		}
	}}
	login.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the approval URL without opening a browser")
	logout := &cobra.Command{Use: "logout", Short: "Revoke this CLI session and remove its keychain token", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		origins := []string{o.origin}
		if o.legacyOrigin != "" && o.legacyOrigin != o.origin {
			origins = append(origins, o.legacyOrigin)
		}
		revoked := map[string]bool{}
		for _, origin := range origins {
			token := a.Token
			if origin != o.origin {
				token, e = o.deps.getToken(origin)
				if errors.Is(e, credentials.ErrNotFound) {
					continue
				}
				if e != nil {
					return e
				}
			}
			if !revoked[token] {
				a.Token = token
				if e = a.Do(cmd.Context(), "POST", "/logout", map[string]any{}, nil); e != nil {
					var apiErr *api.Error
					if !errors.As(e, &apiErr) || apiErr.Status != 401 {
						return e
					}
				}
				revoked[token] = true
			}
			if e := o.deps.deleteToken(origin); e != nil {
				return e
			}
		}
		return nil
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
