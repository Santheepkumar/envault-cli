package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/Santheepkumar/envault-cli/internal/api"
	"github.com/Santheepkumar/envault-cli/internal/config"
	"github.com/Santheepkumar/envault-cli/internal/credentials"
	vcrypto "github.com/Santheepkumar/envault-cli/internal/crypto"
	"github.com/Santheepkumar/envault-cli/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"os"
	"strings"
)

type options struct {
	origin, project, environment string
	passwordStdin                bool
	envExplicit                  bool
}

func New(version string) *cobra.Command {
	o := &options{origin: "http://127.0.0.1:3000", environment: "dev"}
	if c, e := config.Load(); e == nil {
		o.origin = strings.TrimRight(c.APIURL, "/")
		o.project = c.ProjectID
	}
	root := &cobra.Command{Use: "envault", Short: "Encrypted environment secrets for your team", Version: version, SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&o.origin, "api-url", o.origin, "EnVault application origin")
	root.PersistentFlags().StringVar(&o.project, "project", o.project, "Project UUID")
	root.PersistentFlags().StringVar(&o.environment, "env", "dev", "Environment: dev, staging, prod")
	root.PersistentFlags().BoolVar(&o.passwordStdin, "password-stdin", false, "Read the vault password from one line of stdin")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		o.origin = strings.TrimRight(o.origin, "/")
		o.envExplicit = cmd.Flags().Changed("env")
		if e := config.ValidateURL(o.origin); e != nil {
			return e
		}
		if o.environment != "dev" && o.environment != "staging" && o.environment != "prod" {
			return errors.New("invalid environment")
		}
		return nil
	}
	root.AddCommand(authCommands(o)...)
	root.AddCommand(syncCommands(o)...)
	root.AddCommand(accessCommands(o)...)
	return root
}
func password(o *options, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt+": ")
	if o.passwordStdin {
		line, e := bufio.NewReader(os.Stdin).ReadString('\n')
		if e != nil && len(line) == 0 {
			return "", e
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("vault password requires a terminal; use --password-stdin for explicit stdin input")
	}
	b, e := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	defer vcrypto.Clear(b)
	return string(b), e
}
func humanAPI(o *options) (*api.Client, error) {
	t, e := credentials.Get(o.origin)
	if e != nil {
		return nil, e
	}
	return api.New(o.origin, t)
}
func unlocked(ctx context.Context, o *options) (*vault.Client, error) {
	if raw := os.Getenv("ENVAULT_MACHINE"); raw != "" {
		b, e := vault.ParseMachine(raw)
		if e != nil {
			return nil, e
		}
		if o.project != "" && o.project != b.ProjectID {
			return nil, errors.New("machine credential project mismatch")
		}
		if o.envExplicit && o.environment != b.Environment {
			return nil, errors.New("machine credential environment mismatch")
		}
		a, e := api.New(b.APIURL, b.Token)
		if e != nil {
			return nil, e
		}
		key, e := vcrypto.Decode(b.PrivateKey)
		if e != nil {
			return nil, e
		}
		return &vault.Client{API: a, PrivateKey: key, ProjectID: b.ProjectID, Environment: b.Environment, MachineID: b.ID}, nil
	}
	a, e := humanAPI(o)
	if e != nil {
		return nil, e
	}
	var result struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Account  *vcrypto.Account `json:"account"`
		Revision int              `json:"revision"`
	}
	if e = a.Do(ctx, "GET", "/account", nil, &result); e != nil {
		return nil, e
	}
	if result.Account == nil {
		return nil, errors.New("complete vault onboarding in the dashboard first")
	}
	pw, e := password(o, "Vault password")
	if e != nil {
		return nil, e
	}
	s, e := vcrypto.Unlock(result.User.ID, *result.Account, pw)
	if e != nil {
		return nil, e
	}
	return &vault.Client{API: a, UserID: result.User.ID, PrivateKey: s.PrivateKey, PublicKey: s.Account.PublicKey, VaultKey: s.VaultKey, Account: s.Account, AccountRevision: result.Revision, ProjectID: o.project, Environment: o.environment}, nil
}
func projectRequired(c *vault.Client) error {
	if c.ProjectID == "" {
		return errors.New("select a project with envault init or --project")
	}
	return nil
}
func printJSON(v any) error { return writeJSON(os.Stdout, v) }
