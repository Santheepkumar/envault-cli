package commands

import (
	"fmt"
	"github.com/envault/cli/internal/config"
	vcrypto "github.com/envault/cli/internal/crypto"
	"github.com/envault/cli/internal/process"
	"github.com/envault/cli/internal/vault"
	"github.com/spf13/cobra"
)

func syncCommands(o *options) []*cobra.Command {
	init := &cobra.Command{Use: "init", Short: "Link this directory to a project, or create one", Args: cobra.NoArgs}
	var name string
	init.Flags().StringVar(&name, "name", "", "Create a project with this name")
	init.RunE = func(cmd *cobra.Command, _ []string) error {
		if name != "" {
			c, e := unlocked(cmd.Context(), o)
			if e != nil {
				return e
			}
			defer c.Close()
			id, e := c.CreateProject(cmd.Context(), name)
			if e != nil {
				return e
			}
			o.project = id
		}
		if o.project == "" {
			return fmt.Errorf("provide --project or --name")
		}
		return config.Save(config.Project{APIURL: o.origin, ProjectID: o.project})
	}
	projects := &cobra.Command{Use: "projects", Short: "List accessible projects", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		var result any
		if e = a.Do(cmd.Context(), "GET", pagePath(cmd, "/projects"), nil, &result); e != nil {
			return e
		}
		return printJSON(result)
	}}
	push := &cobra.Command{Use: "push", Short: "Encrypt and upload an environment file", Args: cobra.NoArgs}
	var pushFile string
	var overwriteRemote bool
	push.Flags().StringVar(&pushFile, "file", ".env", "Source file")
	push.Flags().BoolVar(&overwriteRemote, "overwrite-remote", false, "Explicitly replace the current remote version regardless of the local base")
	push.RunE = func(cmd *cobra.Command, _ []string) error {
		c, e := unlocked(cmd.Context(), o)
		if e != nil {
			return e
		}
		defer c.Close()
		if e = projectRequired(c); e != nil {
			return e
		}
		data, e := vault.ReadFile(pushFile)
		if e != nil {
			return e
		}
		defer vcrypto.Clear(data)
		c.BaseHead, c.BaseKnown, e = config.LoadHead(c.API.BaseURL, c.ProjectID, c.Environment, pushFile)
		if e != nil {
			return e
		}
		c.OverwriteRemote = overwriteRemote
		v, e := c.Push(cmd.Context(), data)
		if e != nil {
			return e
		}
		if e = config.SaveHead(c.API.BaseURL, c.ProjectID, c.Environment, pushFile, &v.RevisionID); e != nil {
			return fmt.Errorf("push succeeded but local revision could not be recorded: %w", e)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Pushed %s version %d\n", c.Environment, v.Version)
		return nil
	}
	pull := &cobra.Command{Use: "pull", Short: "Decrypt an environment to a protected file", Args: cobra.NoArgs}
	var output, revision string
	var force bool
	pull.Flags().StringVar(&output, "file", ".env", "Output file")
	pull.Flags().StringVar(&revision, "version", "", "Revision UUID (default latest)")
	pull.Flags().BoolVar(&force, "force", false, "Confirm replacement of an existing regular file")
	pull.RunE = func(cmd *cobra.Command, _ []string) error {
		c, e := unlocked(cmd.Context(), o)
		if e != nil {
			return e
		}
		defer c.Close()
		if e = projectRequired(c); e != nil {
			return e
		}
		data, v, e := c.Read(cmd.Context(), revision)
		if e != nil {
			return e
		}
		defer vcrypto.Clear(data)
		if v == nil {
			return fmt.Errorf("environment has no secret versions")
		}
		if e = vault.WriteFile(output, data, force); e != nil {
			return e
		}
		if e = config.SaveHead(c.API.BaseURL, c.ProjectID, c.Environment, output, &v.RevisionID); e != nil {
			return fmt.Errorf("pull succeeded but local revision could not be recorded: %w", e)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Pulled %s version %d to %s\n", c.Environment, v.Version, output)
		return nil
	}
	run := &cobra.Command{Use: "run -- command [args...]", Short: "Inject secrets into a child process without writing a file", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.ArgsLenAtDash() != 0 {
			return fmt.Errorf("place the child command after --")
		}
		c, e := unlocked(cmd.Context(), o)
		if e != nil {
			return e
		}
		defer c.Close()
		if e = projectRequired(c); e != nil {
			return e
		}
		data, v, e := c.Read(cmd.Context(), "")
		if e != nil {
			return e
		}
		defer vcrypto.Clear(data)
		if v == nil {
			return fmt.Errorf("environment has no secret versions")
		}
		values, e := process.ParseEnv(data)
		if e != nil {
			return e
		}
		return process.Run(cmd.Context(), args, values)
	}}
	history := &cobra.Command{Use: "history", Short: "List encrypted version metadata", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		a, e := humanAPI(o)
		if e != nil {
			return e
		}
		if o.project == "" {
			return fmt.Errorf("select a project first")
		}
		var result any
		if e = a.Do(cmd.Context(), "GET", pagePath(cmd, "/projects/"+o.project+"/environments/"+o.environment+"/versions"), nil, &result); e != nil {
			return e
		}
		return printJSON(result)
	}}
	restore := &cobra.Command{Use: "restore REVISION_UUID", Short: "Re-encrypt a historical version as the current version", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, e := unlocked(cmd.Context(), o)
		if e != nil {
			return e
		}
		defer c.Close()
		if e = projectRequired(c); e != nil {
			return e
		}
		data, v, e := c.Read(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		defer vcrypto.Clear(data)
		if v == nil {
			return fmt.Errorf("version not found")
		}
		c.OverwriteRemote = true
		next, e := c.Push(cmd.Context(), data)
		if e != nil {
			return e
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Restored as version %d\n", next.Version)
		return nil
	}}
	addPagination(projects, true)
	addPagination(history, false)
	return []*cobra.Command{init, projects, push, pull, run, history, restore}
}
