package commands

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/envault/cli/internal/api"
	"github.com/spf13/cobra"
)

var uuidCursor = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func addPagination(cmd *cobra.Command, list bool) {
	cmd.Flags().String("before", "", "Continue from nextCursor in the preceding response")
	if list {
		cmd.Flags().Int("limit", 50, "Results per page (1-100)")
	}
	handler := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		before, _ := cmd.Flags().GetString("before")
		if before != "" {
			if list {
				if !uuidCursor.MatchString(before) {
					return fmt.Errorf("--before must be a UUID cursor")
				}
			} else {
				n, err := strconv.ParseInt(before, 10, 32)
				if err != nil || n < 1 {
					return fmt.Errorf("--before must be a positive version cursor")
				}
			}
		}
		if list {
			n, _ := cmd.Flags().GetInt("limit")
			if n < 1 || n > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}
		}
		return handler(cmd, args)
	}
}

func pagePath(cmd *cobra.Command, path string) string {
	query := url.Values{}
	if before, _ := cmd.Flags().GetString("before"); before != "" {
		query.Set("before", before)
	}
	if cmd.Flags().Lookup("limit") != nil {
		limit, _ := cmd.Flags().GetInt("limit")
		query.Set("limit", strconv.Itoa(limit))
	}
	if len(query) == 0 {
		return path
	}
	return path + "?" + query.Encode()
}

func findMember(ctx context.Context, client *api.Client, projectID, userID string) (member, error) {
	path := "/projects/" + projectID + "/members?limit=100"
	cursor := ""
	for {
		var page struct {
			Members    []member `json:"members"`
			NextCursor *string  `json:"nextCursor"`
		}
		next := path
		if cursor != "" {
			next += "&before=" + url.QueryEscape(cursor)
		}
		if err := client.Do(ctx, "GET", next, nil, &page); err != nil {
			return member{}, err
		}
		for _, m := range page.Members {
			if m.UserID == userID {
				return m, nil
			}
		}
		if page.NextCursor == nil {
			return member{}, fmt.Errorf("member not found")
		}
		if *page.NextCursor == cursor || !uuidCursor.MatchString(*page.NextCursor) {
			return member{}, fmt.Errorf("invalid pagination cursor")
		}
		cursor = *page.NextCursor
	}
}
