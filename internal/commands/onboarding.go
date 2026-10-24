package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Santheepkumar/envault-cli/internal/api"
	"github.com/Santheepkumar/envault-cli/internal/config"
	"github.com/Santheepkumar/envault-cli/internal/credentials"
	"github.com/Santheepkumar/envault-cli/internal/vault"
	"github.com/spf13/cobra"
)

func openBrowser(uri string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", uri)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", uri)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", uri)
	}
	return cmd.Run()
}

func validateVerificationURL(raw, origin string) error {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" {
		return errors.New("invalid device verification URL")
	}
	base, err := config.NormalizeURL(u.Scheme + "://" + u.Host)
	if err != nil || base != origin {
		return errors.New("device verification URL belongs to a different installation")
	}
	return nil
}

func readAnswer(cmd *cobra.Command, reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("setup cancelled: %w", err)
	}
	return strings.TrimSpace(line), nil
}

type projectChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func chooseProject(cmd *cobra.Command, o *options, reader *bufio.Reader) (string, string, error) {
	a, err := humanAPI(o)
	if err != nil {
		return "", "", err
	}
	cursor := ""
	seen := map[string]bool{}
	for {
		path := "/projects?limit=50"
		if cursor != "" {
			path += "&before=" + url.QueryEscape(cursor)
		}
		var page struct {
			Projects   []projectChoice `json:"projects"`
			NextCursor *string         `json:"nextCursor"`
		}
		if err := a.Do(cmd.Context(), "GET", path, nil, &page); err != nil {
			return "", "", err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Projects on %s:\n", o.origin)
		for i, p := range page.Projects {
			fmt.Fprintf(cmd.ErrOrStderr(), "%d) %s (%s)\n", i+1, p.Name, p.ID)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "c) Create a project\nq) Cancel")
		if page.NextCursor != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "n) Next page")
		}
		for {
			answer, err := readAnswer(cmd, reader, "Select a project: ")
			if err != nil {
				return "", "", err
			}
			switch strings.ToLower(answer) {
			case "q", "":
				return "", "", errors.New("setup cancelled")
			case "c":
				name, err := readAnswer(cmd, reader, "Project name: ")
				if err != nil {
					return "", "", err
				}
				if name == "" || len(name) > 100 {
					return "", "", errors.New("project name must contain 1-100 bytes")
				}
				return "", name, nil
			case "n":
				if page.NextCursor == nil {
					continue
				}
				if !uuidCursor.MatchString(*page.NextCursor) || seen[*page.NextCursor] {
					return "", "", errors.New("invalid project pagination cursor")
				}
				cursor = *page.NextCursor
				seen[cursor] = true
			default:
				n, err := strconv.Atoi(answer)
				if err != nil || n < 1 || n > len(page.Projects) {
					fmt.Fprintln(cmd.ErrOrStderr(), "Choose a listed number, c, n, or q.")
					continue
				}
				return page.Projects[n-1].ID, "", nil
			}
			break
		}
	}
}

func initCommand(o *options) *cobra.Command {
	var name string
	var force bool
	cmd := &cobra.Command{Use: "init", Short: "Choose a project to link to this directory, or create one", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&name, "name", "", "Create a project with this name")
	cmd.Flags().BoolVar(&force, "force", false, "Confirm replacing an existing project binding")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		explicit := cmd.Flags().Changed("project")
		if explicit && cmd.Flags().Changed("name") {
			return errors.New("--project and --name are mutually exclusive")
		}
		if explicit && o.project == "" {
			return errors.New("--project requires a project UUID")
		}
		if cmd.Flags().Changed("name") && (strings.TrimSpace(name) == "" || len(name) > 100) {
			return errors.New("project name must contain 1-100 bytes")
		}
		if os.Getenv("ENVAULT_MACHINE") != "" {
			return errors.New("init requires a human login; machine credentials already contain their project binding")
		}
		path := o.configPath
		if path == "" {
			path = ".envault.json"
		}
		bound, _ := config.NormalizeURL(o.binding.APIURL)
		same := o.configPath != "" && bound == o.origin && name == "" && (explicit && o.project == o.binding.ProjectID || !explicit && !force)
		if same && !explicit {
			fmt.Fprintln(cmd.OutOrStdout(), "Project already linked:", o.binding.ProjectID)
			return nil
		}
		interactive := o.deps.terminal(cmd.InOrStdin())
		reader := o.reader
		if !interactive && !explicit && name == "" {
			return errors.New("noninteractive init requires --project UUID or --name NAME")
		}
		if o.configPath != "" && !same && !force {
			if !interactive {
				return errors.New("project already linked; use init --force to replace its binding")
			}
			answer, err := readAnswer(cmd, reader, "Replace the existing project binding? [y/N]: ")
			if err != nil {
				return err
			}
			if strings.ToLower(answer) != "y" && strings.ToLower(answer) != "yes" {
				return errors.New("setup cancelled")
			}
		}
		id := o.project
		if !explicit && name == "" {
			var err error
			id, name, err = chooseProject(cmd, o, reader)
			if err != nil {
				return err
			}
		}
		if name != "" {
			c, err := unlocked(cmd.Context(), o)
			if err != nil {
				return err
			}
			defer c.Close()
			id, err = c.CreateProject(cmd.Context(), name)
			if err != nil {
				return err
			}
		} else {
			if !uuidCursor.MatchString(id) {
				return errors.New("project must be a UUID")
			}
			a, err := humanAPI(o)
			if err != nil {
				return err
			}
			var result struct {
				Project projectChoice `json:"project"`
			}
			if err = a.Do(cmd.Context(), "GET", "/projects/"+id, nil, &result); err != nil {
				return err
			}
			if result.Project.ID != id {
				return errors.New("server returned a mismatching project")
			}
		}
		if o.configPath != "" && bound == o.origin && id == o.binding.ProjectID {
			fmt.Fprintln(cmd.OutOrStdout(), "Project already linked:", id)
			return nil
		}
		if err := config.SaveProject(path, config.Project{APIURL: o.origin, ProjectID: id}, o.configPath != ""); err != nil {
			return fmt.Errorf("could not save project binding (project %s on %s): %w", id, o.origin, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Linked project %s on %s.\nRun envault pull to fetch the dev environment.\n", id, o.origin)
		return nil
	}
	return cmd
}

func statusCommand(o *options) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show installation, project and authentication status without unlocking", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		fmt.Fprintf(cmd.OutOrStdout(), "Installation: %s\nSource: %s\n", o.origin, o.source)
		if raw := os.Getenv("ENVAULT_MACHINE"); raw != "" {
			b, err := vault.ParseMachine(raw)
			if err != nil {
				return err
			}
			if o.project != "" && o.project != b.ProjectID {
				return errors.New("machine credential project mismatch")
			}
			if o.envExplicit && o.environment != b.Environment {
				return errors.New("machine credential environment mismatch")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Project: %s\nAuthentication: machine credential configured (not checked remotely)\n", b.ProjectID)
			return nil
		}
		project := o.project
		if o.configPath != "" && !cmd.Flags().Changed("project") {
			bound, _ := config.NormalizeURL(o.binding.APIURL)
			if bound != o.origin {
				project = ""
			}
		}
		if project == "" {
			project = "not linked; run envault init"
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Project:", project)
		a, err := humanAPI(o)
		if err != nil {
			if errors.Is(err, credentials.ErrNotFound) {
				fmt.Fprintln(cmd.OutOrStdout(), "Authentication: not logged in")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Authentication: keychain unavailable")
			}
			return err
		}
		err = a.Do(cmd.Context(), "GET", "/account", nil, nil)
		if err != nil {
			var apiErr *api.Error
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
				fmt.Fprintln(cmd.OutOrStdout(), "Authentication: session expired or revoked")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Authentication: could not verify (server unreachable or unavailable)")
			}
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Authentication: authenticated")
		return nil
	}}
}
