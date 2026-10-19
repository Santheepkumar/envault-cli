package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/envault/cli/internal/api"
	"github.com/spf13/cobra"
)

func TestPaginationFlags(t *testing.T) {
	for _, args := range [][]string{{"--limit=0"}, {"--limit=101"}, {"--before=invalid"}} {
		cmd := &cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { t.Error("invalid flags reached handler"); return nil }}
		addPagination(cmd, true)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	cmd := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, _ []string) error {
		if got := pagePath(cmd, "/projects"); got != "/projects?before=7b2d19cc-933b-4ded-a65e-4b17fecc720b&limit=2" {
			t.Errorf("path %s", got)
		}
		return nil
	}}
	addPagination(cmd, true)
	cmd.SetArgs([]string{"--limit=2", "--before=7b2d19cc-933b-4ded-a65e-4b17fecc720b"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestFindMemberFollowsPages(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		cursor := "7b2d19cc-933b-4ded-a65e-4b17fecc720b"
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"members": []member{}, "nextCursor": cursor})
			return
		}
		if r.URL.Query().Get("before") != cursor {
			t.Error("cursor missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"members": []member{{UserID: "target", Name: "Alice"}}, "nextCursor": nil})
	}))
	defer server.Close()
	client, _ := api.New(server.URL, "")
	m, err := findMember(context.Background(), client, "project", "target")
	if err != nil || m.Name != "Alice" || calls != 2 {
		t.Fatalf("member=%+v calls=%d err=%v", m, calls, err)
	}
}
