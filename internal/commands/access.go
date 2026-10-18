package commands

import (
	"fmt"
	"github.com/envault/cli/internal/config"
	vcrypto "github.com/envault/cli/internal/crypto"
	"github.com/envault/cli/internal/vault"
	"github.com/spf13/cobra"
	"os"
	"strings"
)

type member struct {
	UserID    string `json:"userId"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	PublicKey string `json:"publicKey"`
}

func accessCommands(o *options) []*cobra.Command {
	members := &cobra.Command{Use: "members", Short: "Manage project membership"}
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		var r any
		if e = a.Do(cmd.Context(), "GET", "/projects/"+o.project+"/members", nil, &r); e != nil {
			return e
		}
		return printJSON(r)
	}}
	invite := &cobra.Command{Use: "invite GITHUB_NUMERIC_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		var r any
		if e = a.Do(cmd.Context(), "POST", "/projects/"+o.project+"/invitations", map[string]string{"githubId": args[0]}, &r); e != nil {
			return e
		}
		return printJSON(r)
	}}
	accept := &cobra.Command{Use: "accept INVITATION_TOKEN", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		var r any
		if e = a.Do(cmd.Context(), "POST", "/invitations/accept", map[string]string{"token": args[0]}, &r); e != nil {
			return e
		}
		return printJSON(r)
	}}
	grant := &cobra.Command{Use: "grant USER_UUID", Short: "Grant verified environment access", Args: cobra.ExactArgs(1)}
	var fp string
	var write bool
	grant.Flags().StringVar(&fp, "fingerprint", "", "Fingerprint verified through a separate channel")
	grant.Flags().BoolVar(&write, "write", false, "Allow pushes")
	_ = grant.MarkFlagRequired("fingerprint")
	grant.RunE = func(cmd *cobra.Command, args []string) error {
		c, e := unlocked(cmd.Context(), o)
		if e != nil {
			return e
		}
		defer c.Close()
		var r struct {
			Members []member `json:"members"`
		}
		if e = c.API.Do(cmd.Context(), "GET", "/projects/"+c.ProjectID+"/members", nil, &r); e != nil {
			return e
		}
		for _, m := range r.Members {
			if m.UserID != args[0] {
				continue
			}
			actual, e := vcrypto.Fingerprint(m.PublicKey)
			if e != nil {
				return e
			}
			if strings.ReplaceAll(fp, " ", "") != strings.ReplaceAll(actual, " ", "") {
				return fmt.Errorf("fingerprint does not match")
			}
			if e = config.Pin(o.origin, m.UserID, actual); e != nil {
				return e
			}
			env, key, e := c.GetEnvironment(cmd.Context(), 0)
			if e != nil {
				return e
			}
			defer vcrypto.Clear(key)
			w, e := vcrypto.WrapKey(key, m.PublicKey, env.Context, m.UserID)
			if e != nil {
				return e
			}
			return c.API.Do(cmd.Context(), "POST", c.Path()+"/grants", map[string]any{"userId": m.UserID, "write": write, "verifiedPublicKey": m.PublicKey, "epoch": env.Epoch, "wrap": w}, nil)
		}
		return fmt.Errorf("member not found")
	}
	remove := &cobra.Command{Use: "remove USER_UUID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		return a.Do(cmd.Context(), "DELETE", "/projects/"+o.project+"/members/"+args[0], nil, nil)
	}}
	role := &cobra.Command{Use: "role USER_UUID owner|admin|developer", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		return a.Do(cmd.Context(), "PATCH", "/projects/"+o.project+"/members/"+args[0], map[string]string{"role": args[1]}, nil)
	}}
	confirm := &cobra.Command{Use: "confirm-ownership", Short: "Verify local access before ownership transfer", RunE: func(cmd *cobra.Command, _ []string) error {
		c, e := unlocked(cmd.Context(), o)
		if e != nil {
			return e
		}
		defer c.Close()
		confirmed := map[string]any{}
		for _, env := range []string{"dev", "staging", "prod"} {
			c.Environment = env
			state, k, e := c.GetEnvironment(cmd.Context(), 0)
			if e != nil {
				return e
			}
			vcrypto.Clear(k)
			confirmed[env] = map[string]int{"epoch": state.Epoch, "accessRevision": state.AccessRevision}
		}
		return c.API.Do(cmd.Context(), "POST", "/projects/"+c.ProjectID+"/confirm-owner-access", confirmed, nil)
	}}
	members.AddCommand(list, invite, accept, grant, remove, role, confirm)
	credentials := &cobra.Command{Use: "credentials", Short: "Manage environment-scoped machine credentials"}
	create := &cobra.Command{Use: "create LABEL", Args: cobra.ExactArgs(1)}
	var days int
	create.Flags().IntVar(&days, "days", 30, "Expiration in days (1-365)")
	create.RunE = func(cmd *cobra.Command, args []string) error {
		c, e := unlocked(cmd.Context(), o)
		if e != nil {
			return e
		}
		defer c.Close()
		env, key, e := c.GetEnvironment(cmd.Context(), 0)
		if e != nil {
			return e
		}
		defer vcrypto.Clear(key)
		private := vcrypto.RandomKey()
		defer vcrypto.Clear(private)
		pub, e := vcrypto.PublicKey(private)
		if e != nil {
			return e
		}
		id := vault.UUID()
		w, e := vcrypto.WrapKey(key, vcrypto.B64(pub), env.Context, id)
		if e != nil {
			return e
		}
		var result struct {
			Token string `json:"token"`
		}
		if e = c.API.Do(cmd.Context(), "POST", c.Path()+"/credentials", map[string]any{"id": id, "label": args[0], "publicKey": vcrypto.B64(pub), "epoch": env.Epoch, "wrap": w, "expiresInDays": days}, &result); e != nil {
			return e
		}
		fmt.Fprintln(os.Stderr, "Store this bundle securely as ENVAULT_MACHINE. It is displayed only once.")
		return printJSON(vault.MachineBundle{V: 1, APIURL: o.origin, Token: result.Token, PrivateKey: vcrypto.B64(private), ProjectID: c.ProjectID, Environment: c.Environment, ID: id})
	}
	clist := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		var r any
		if e = a.Do(cmd.Context(), "GET", "/projects/"+o.project+"/credentials", nil, &r); e != nil {
			return e
		}
		return printJSON(r)
	}}
	revoke := &cobra.Command{Use: "revoke CREDENTIAL_UUID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		return a.Do(cmd.Context(), "DELETE", "/projects/"+o.project+"/credentials/"+args[0], nil, nil)
	}}
	credentials.AddCommand(create, clist, revoke)
	rotate := &cobra.Command{Use: "rotate", Short: "Rotate this environment and re-encrypt its current secrets", Args: cobra.NoArgs}
	var verified bool
	rotate.Flags().BoolVar(&verified, "verified-recipients", false, "Confirm all displayed recipient fingerprints through a separate channel")
	rotate.RunE = func(cmd *cobra.Command, _ []string) error {
		c, e := unlocked(cmd.Context(), o)
		if e != nil {
			return e
		}
		defer c.Close()
		var r struct {
			AccessRevision int     `json:"accessRevision"`
			ExpectedHead   *string `json:"expectedHead"`
			Epoch          int     `json:"epoch"`
			Recipients     []struct {
				ID        string `json:"id"`
				PublicKey string `json:"publicKey"`
			} `json:"recipients"`
		}
		if e = c.API.Do(cmd.Context(), "GET", c.Path()+"/rotation", nil, &r); e != nil {
			return e
		}
		for _, recipient := range r.Recipients {
			f, e := vcrypto.Fingerprint(recipient.PublicKey)
			if e != nil {
				return e
			}
			fmt.Fprintf(os.Stderr, "%s  %s\n", recipient.ID, f)
			if verified {
				if e = config.Pin(o.origin, recipient.ID, f); e != nil {
					return e
				}
			}
		}
		if !verified {
			return fmt.Errorf("verify all recipients, then rerun with --verified-recipients")
		}
		data, v, e := c.Read(cmd.Context(), "")
		if e != nil {
			return e
		}
		defer vcrypto.Clear(data)
		newKey := vcrypto.RandomKey()
		defer vcrypto.Clear(newKey)
		ctx := vcrypto.Context{ProjectID: c.ProjectID, Environment: c.Environment, Epoch: r.Epoch + 1}
		revision := vault.UUID()
		var box *vcrypto.Box
		if v != nil {
			b, e := vcrypto.Seal(data, newKey, vcrypto.SecretContext(ctx, revision))
			if e != nil {
				return e
			}
			box = &b
		}
		wraps := []map[string]any{}
		for _, recipient := range r.Recipients {
			w, e := vcrypto.WrapKey(newKey, recipient.PublicKey, ctx, recipient.ID)
			if e != nil {
				return e
			}
			wraps = append(wraps, map[string]any{"recipientId": recipient.ID, "wrap": w})
		}
		return c.API.Do(cmd.Context(), "POST", c.Path()+"/rotation", map[string]any{"accessRevision": r.AccessRevision, "expectedHead": r.ExpectedHead, "epoch": ctx.Epoch, "revisionId": revision, "box": box, "wraps": wraps}, nil)
	}
	return []*cobra.Command{members, credentials, rotate}
}
