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
	"io"
	"os"
	"strings"
	"time"
)

type commandDeps struct {
	getToken     func(string) (string, error)
	setToken     func(string, string) error
	deleteToken  func(string) error
	terminal     func(io.Reader) bool
	openBrowser  func(string) error
	pollInterval time.Duration
}

type options struct {
	origin, project, environment     string
	passwordStdin                    bool
	envExplicit                      bool
	source, configPath, legacyOrigin string
	binding                          config.Project
	deps                             commandDeps
	input                            io.Reader
	reader                           *bufio.Reader
	stderr                           io.Writer
}

func New(version string) *cobra.Command {
	return newCommand(version, commandDeps{
		getToken: credentials.Get, setToken: credentials.Set, deleteToken: credentials.Delete,
		terminal: terminalInput, openBrowser: openBrowser, pollInterval: 5 * time.Second,
	})
}

func newCommand(version string, deps commandDeps) *cobra.Command {
	o := &options{origin: config.DefaultOrigin, environment: "dev", deps: deps}
	root := &cobra.Command{Use: "envault", Short: "Encrypted environment secrets for your team", Version: version, SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&o.origin, "api-url", o.origin, "EnVault application origin")
	root.PersistentFlags().StringVar(&o.project, "project", o.project, "Project UUID")
	root.PersistentFlags().StringVar(&o.environment, "env", "dev", "Environment: dev, staging, prod")
	root.PersistentFlags().BoolVar(&o.passwordStdin, "password-stdin", false, "Read the vault password from one line of stdin")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		o.input, o.stderr = cmd.InOrStdin(), cmd.ErrOrStderr()
		o.reader = bufio.NewReader(o.input)
		o.envExplicit = cmd.Flags().Changed("env")
		if e := resolveOptions(cmd, o); e != nil {
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
	root.AddCommand(statusCommand(o))
	return root
}

func terminalInput(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func resolveOptions(cmd *cobra.Command, o *options) error {
	c, path, err := config.Find()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	o.binding, o.configPath = c, path
	if !cmd.Flags().Changed("project") {
		o.project = c.ProjectID
	}
	o.source = "hosted default"
	// Machine bundles own their installation; human defaults must not interfere.
	if raw := os.Getenv("ENVAULT_MACHINE"); raw != "" && (cmd.Name() == "pull" || cmd.Name() == "push" || cmd.Name() == "run" || cmd.Name() == "status") {
		b, err := vault.ParseMachine(raw)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("api-url") {
			explicit, e := config.NormalizeURL(o.origin)
			bundle, be := config.NormalizeURL(b.APIURL)
			if e != nil || be != nil || explicit != bundle {
				return errors.New("machine credential installation mismatch")
			}
		}
		o.origin, o.source = b.APIURL, "machine credential"
	} else if cmd.Flags().Changed("api-url") {
		o.source = "--api-url"
	} else if path != "" {
		o.origin, o.source = c.APIURL, path
	} else {
		u, err := config.LoadUser()
		if err != nil {
			return err
		}
		if u.APIURL != "" {
			o.origin, o.source = u.APIURL, "user configuration"
		}
	}
	o.legacyOrigin = strings.TrimRight(o.origin, "/")
	o.origin, err = config.NormalizeURL(o.origin)
	if err != nil {
		return err
	}
	if path != "" {
		bound, _ := config.NormalizeURL(c.APIURL)
		if bound == o.origin && strings.TrimRight(c.APIURL, "/") != o.origin {
			o.legacyOrigin = strings.TrimRight(c.APIURL, "/")
		}
	}
	if o.legacyOrigin != o.origin {
		if err := config.MigratePins(o.legacyOrigin, o.origin); err != nil {
			return err
		}
	}
	if path != "" && cmd.Flags().Changed("api-url") && !cmd.Flags().Changed("project") && projectOperation(cmd) {
		bound, _ := config.NormalizeURL(c.APIURL)
		if bound != o.origin {
			return errors.New("installation differs from linked project; use envault init to relink, or explicitly provide both --api-url and --project")
		}
	}
	return nil
}

func projectOperation(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "login", "logout", "recover", "projects", "init", "status":
		return false
	}
	return true
}

func loginHint(origin string) string {
	if origin == config.DefaultOrigin {
		return "run envault login"
	}
	return "run envault login --api-url " + origin
}
func password(o *options, prompt string) (string, error) {
	if o.input == nil {
		o.input = os.Stdin
		o.reader = bufio.NewReader(o.input)
	}
	if o.stderr == nil {
		o.stderr = os.Stderr
	}
	fmt.Fprint(o.stderr, prompt+": ")
	if o.passwordStdin {
		line, e := o.reader.ReadString('\n')
		if e != nil && len(line) == 0 {
			return "", e
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	f, ok := o.input.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return "", errors.New("vault password requires a terminal; use --password-stdin for explicit stdin input")
	}
	b, e := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(o.stderr)
	defer vcrypto.Clear(b)
	return string(b), e
}
func humanAPI(o *options) (*api.Client, error) {
	t, e := o.deps.getToken(o.origin)
	if errors.Is(e, credentials.ErrNotFound) && o.legacyOrigin != "" && o.legacyOrigin != o.origin {
		t, e = o.deps.getToken(o.legacyOrigin)
		if e == nil {
			if e = o.deps.setToken(o.origin, t); e != nil {
				return nil, fmt.Errorf("could not migrate legacy keychain credential: %w", e)
			}
			if e = o.deps.deleteToken(o.legacyOrigin); e != nil {
				return nil, fmt.Errorf("credential migrated but could not remove legacy keychain entry: %w", e)
			}
		}
	}
	if e != nil {
		return nil, fmt.Errorf("%s; %w", loginHint(o.origin), e)
	}
	a, e := api.New(o.origin, t)
	if e == nil {
		a.LoginHint = loginHint(o.origin)
	}
	return a, e
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
