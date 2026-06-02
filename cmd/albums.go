package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/theinventor/footagepal-cli/internal/exitcode"
)

type albumsResponse struct {
	Albums     []contentMap   `json:"albums"`
	Pagination map[string]any `json:"pagination,omitempty"`
	Links      map[string]any `json:"links,omitempty"`
}

type albumContentsResponse struct {
	Album      contentMap     `json:"album,omitempty"`
	Contents   []contentMap   `json:"contents"`
	Pagination map[string]any `json:"pagination,omitempty"`
	Links      map[string]any `json:"links,omitempty"`
}

type albumUsersResponse struct {
	Album contentMap     `json:"album,omitempty"`
	Users []contentMap   `json:"users"`
	Links map[string]any `json:"links,omitempty"`
}

func newAlbumsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "albums",
		Aliases: []string{"album"},
		Short:   "Create, inspect, and manage FootagePal albums",
	}
	c.AddCommand(newAlbumsListCmd())
	c.AddCommand(newAlbumsShowCmd())
	c.AddCommand(newAlbumsCreateCmd())
	c.AddCommand(newAlbumsUpdateCmd())
	c.AddCommand(newAlbumContentsCmd())
	c.AddCommand(newAlbumAccessCmd())
	return c
}

func newAlbumsListCmd() *cobra.Command {
	var accountID, archived string
	var page, perPage int
	var human bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List albums visible to the active token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if accountID != "" {
				q.Set("account_id", accountID)
			}
			if archived != "" {
				value, err := normalizeBoolString(archived, "--archived")
				if err != nil {
					return err
				}
				q.Set("archived", value)
			}
			if page < 0 || perPage < 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--page and --per-page must be >= 0"))
			}
			if page > 0 {
				q.Set("page", strconv.Itoa(page))
			}
			if perPage > 0 {
				q.Set("per_page", strconv.Itoa(perPage))
			}
			resp, err := newAPIClient().Do(http.MethodGet, "/api/v1/albums", nil, q)
			if err != nil {
				return err
			}
			var body albumsResponse
			if err := decodeAPIResponse(resp, &body); err != nil {
				return err
			}
			if human {
				printAlbumTable(cmd.OutOrStdout(), body.Albums)
				return nil
			}
			return printJSON(cmd.OutOrStdout(), body)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().StringVar(&archived, "archived", "", "filter archived albums: true or false")
	c.Flags().IntVar(&page, "page", 0, "1-based page number")
	c.Flags().IntVar(&perPage, "per-page", 0, "page size, max 100")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumsShowCmd() *cobra.Command {
	var accountID string
	var human bool
	c := &cobra.Command{
		Use:   "show <album-id>",
		Short: "Fetch one album",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := accountQuery(accountID)
			resp, err := newAPIClient().Do(http.MethodGet, "/api/v1/albums/"+url.PathEscape(args[0]), nil, q)
			if err != nil {
				return err
			}
			var body map[string]any
			if err := decodeAPIResponse(resp, &body); err != nil {
				return err
			}
			if human {
				if album, ok := body["album"].(map[string]any); ok {
					printAlbumTable(cmd.OutOrStdout(), []contentMap{album})
					return nil
				}
			}
			return printJSON(cmd.OutOrStdout(), body)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumsCreateCmd() *cobra.Command {
	var accountID, name string
	var archived, human bool
	c := &cobra.Command{
		Use:   "create",
		Short: "Create an album in the selected account",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--name is required"))
			}
			body := albumMutationBody(accountID, name, archived, cmd.Flags().Changed("archived"))
			resp, err := newAPIClient().Do(http.MethodPost, "/api/v1/albums", body, nil)
			if err != nil {
				return err
			}
			return printAlbumMutation(cmd.OutOrStdout(), resp, human)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().StringVar(&name, "name", "", "album name (required)")
	c.Flags().BoolVar(&archived, "archived", false, "create the album as archived")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumsUpdateCmd() *cobra.Command {
	var accountID, name string
	var archived, human bool
	c := &cobra.Command{
		Use:   "update <album-id>",
		Short: "Update an album name or archived state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			hasName := strings.TrimSpace(name) != ""
			hasArchived := cmd.Flags().Changed("archived")
			if !hasName && !hasArchived {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("provide --name or --archived"))
			}
			body := albumMutationBody(accountID, name, archived, hasArchived)
			resp, err := newAPIClient().Do(http.MethodPatch, "/api/v1/albums/"+url.PathEscape(args[0]), body, nil)
			if err != nil {
				return err
			}
			return printAlbumMutation(cmd.OutOrStdout(), resp, human)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().StringVar(&name, "name", "", "album name")
	c.Flags().BoolVar(&archived, "archived", false, "set archived state")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumContentsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "contents",
		Aliases: []string{"content"},
		Short:   "List, attach, and detach album media",
	}
	c.AddCommand(newAlbumContentsListCmd())
	c.AddCommand(newAlbumContentsAddCmd())
	c.AddCommand(newAlbumContentsRemoveCmd())
	return c
}

func newAlbumContentsListCmd() *cobra.Command {
	var flags mediaSearchFlags
	var human bool
	c := &cobra.Command{
		Use:   "list <album-id>",
		Short: "List media attached to an album",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q, err := buildSearchQuery(cmd, flags)
			if err != nil {
				return err
			}
			result, err := fetchAlbumContents(cmd.Context(), newAPIClient(), args[0], q, flags.Limit)
			if err != nil {
				return err
			}
			if human {
				printContentTable(cmd.OutOrStdout(), result.Contents)
				return nil
			}
			return printJSON(cmd.OutOrStdout(), result)
		},
	}
	addSearchFlags(c, &flags, false)
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumContentsAddCmd() *cobra.Command {
	var accountID string
	var human bool
	c := &cobra.Command{
		Use:     "add <album-id> <content-id>",
		Aliases: []string{"attach"},
		Short:   "Attach existing media to an album",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{"content_id": args[1]}
			addAccountToBody(body, accountID)
			resp, err := newAPIClient().Do(http.MethodPost, "/api/v1/albums/"+url.PathEscape(args[0])+"/contents", body, nil)
			if err != nil {
				return err
			}
			return printAlbumContentMutation(cmd.OutOrStdout(), resp, human)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumContentsRemoveCmd() *cobra.Command {
	var accountID string
	var human bool
	c := &cobra.Command{
		Use:     "remove <album-id> <content-id>",
		Aliases: []string{"detach", "rm"},
		Short:   "Detach media from an album",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := accountQuery(accountID)
			path := "/api/v1/albums/" + url.PathEscape(args[0]) + "/contents/" + url.PathEscape(args[1])
			resp, err := newAPIClient().Do(http.MethodDelete, path, nil, q)
			if err != nil {
				return err
			}
			return printAlbumContentMutation(cmd.OutOrStdout(), resp, human)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumAccessCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "access",
		Aliases: []string{"users", "user"},
		Short:   "Manage users allowed to access an album",
	}
	c.AddCommand(newAlbumAccessListCmd())
	c.AddCommand(newAlbumAccessAddCmd())
	c.AddCommand(newAlbumAccessRemoveCmd())
	return c
}

func newAlbumAccessListCmd() *cobra.Command {
	var accountID string
	var human bool
	c := &cobra.Command{
		Use:   "list <album-id>",
		Short: "List account users with album access",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := accountQuery(accountID)
			resp, err := newAPIClient().Do(http.MethodGet, "/api/v1/albums/"+url.PathEscape(args[0])+"/users", nil, q)
			if err != nil {
				return err
			}
			var body albumUsersResponse
			if err := decodeAPIResponse(resp, &body); err != nil {
				return err
			}
			if human {
				printAlbumUserTable(cmd.OutOrStdout(), body.Users)
				return nil
			}
			return printJSON(cmd.OutOrStdout(), body)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumAccessAddCmd() *cobra.Command {
	var accountID, userID, email string
	var human bool
	c := &cobra.Command{
		Use:   "add <album-id>",
		Short: "Grant a current-account user album access by user id or email",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (userID == "" && email == "") || (userID != "" && email != "") {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("provide exactly one of --user-id or --email"))
			}
			body := map[string]any{}
			addAccountToBody(body, accountID)
			if userID != "" {
				body["user_id"] = userID
			}
			if email != "" {
				body["email"] = email
			}
			resp, err := newAPIClient().Do(http.MethodPost, "/api/v1/albums/"+url.PathEscape(args[0])+"/users", body, nil)
			if err != nil {
				return err
			}
			return printAlbumUserMutation(cmd.OutOrStdout(), resp, human)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().StringVar(&userID, "user-id", "", "current-account user id to grant")
	c.Flags().StringVar(&email, "email", "", "current-account user email to grant")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newAlbumAccessRemoveCmd() *cobra.Command {
	var accountID string
	var human bool
	c := &cobra.Command{
		Use:     "remove <album-id> <user-id>",
		Aliases: []string{"rm"},
		Short:   "Remove album access from a current-account user id",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := accountQuery(accountID)
			path := "/api/v1/albums/" + url.PathEscape(args[0]) + "/users/" + url.PathEscape(args[1])
			resp, err := newAPIClient().Do(http.MethodDelete, path, nil, q)
			if err != nil {
				return err
			}
			return printAlbumUserMutation(cmd.OutOrStdout(), resp, human)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func fetchAlbumContents(ctx context.Context, cli doer, albumID string, q url.Values, limit int) (albumContentsResponse, error) {
	path := "/api/v1/albums/" + url.PathEscape(albumID) + "/contents"
	if limit <= 0 {
		resp, err := cli.Do(http.MethodGet, path, nil, q)
		if err != nil {
			return albumContentsResponse{}, err
		}
		var result albumContentsResponse
		if err := decodeAPIResponse(resp, &result); err != nil {
			return albumContentsResponse{}, err
		}
		return result, nil
	}

	page := intFromValues(q, "page", 1)
	perPage := intFromValues(q, "per_page", 100)
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}
	out := albumContentsResponse{Contents: []contentMap{}}
	for len(out.Contents) < limit {
		select {
		case <-ctx.Done():
			return albumContentsResponse{}, ctx.Err()
		default:
		}
		pageQuery := cloneValues(q)
		pageQuery.Set("page", strconv.Itoa(page))
		pageQuery.Set("per_page", strconv.Itoa(perPage))
		resp, err := cli.Do(http.MethodGet, path, nil, pageQuery)
		if err != nil {
			return albumContentsResponse{}, err
		}
		var result albumContentsResponse
		if err := decodeAPIResponse(resp, &result); err != nil {
			return albumContentsResponse{}, err
		}
		out.Album = result.Album
		out.Pagination = result.Pagination
		out.Links = result.Links
		for _, content := range result.Contents {
			if len(out.Contents) >= limit {
				break
			}
			out.Contents = append(out.Contents, content)
		}
		if len(result.Contents) == 0 || !hasNextPage(result.Pagination, page) {
			break
		}
		page++
	}
	if out.Pagination == nil {
		out.Pagination = map[string]any{}
	}
	out.Pagination["client_limit"] = limit
	out.Pagination["returned_count"] = len(out.Contents)
	return out, nil
}

func albumMutationBody(accountID, name string, archived bool, includeArchived bool) map[string]any {
	album := map[string]any{}
	if strings.TrimSpace(name) != "" {
		album["name"] = name
	}
	if includeArchived {
		album["archived"] = archived
	}
	body := map[string]any{"album": album}
	addAccountToBody(body, accountID)
	return body
}

func accountQuery(accountID string) url.Values {
	q := url.Values{}
	if accountID != "" {
		q.Set("account_id", accountID)
	}
	return q
}

func addAccountToBody(body map[string]any, accountID string) {
	if accountID != "" {
		body["account_id"] = accountID
	}
}

func normalizeBoolString(value, flag string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return "true", nil
	case "false", "0", "no":
		return "false", nil
	default:
		return "", exitcode.Wrap(exitcode.Usage, fmt.Errorf("%s must be true or false", flag))
	}
}

func printAlbumMutation(w io.Writer, resp *http.Response, human bool) error {
	var body map[string]any
	if err := decodeAPIResponse(resp, &body); err != nil {
		return err
	}
	if human {
		if album, ok := body["album"].(map[string]any); ok {
			printAlbumTable(w, []contentMap{album})
			return nil
		}
	}
	return printJSON(w, body)
}

func printAlbumContentMutation(w io.Writer, resp *http.Response, human bool) error {
	var body map[string]any
	if err := decodeAPIResponse(resp, &body); err != nil {
		return err
	}
	if human {
		if content, ok := body["content"].(map[string]any); ok {
			printContentTable(w, []contentMap{content})
			return nil
		}
	}
	return printJSON(w, body)
}

func printAlbumUserMutation(w io.Writer, resp *http.Response, human bool) error {
	var body map[string]any
	if err := decodeAPIResponse(resp, &body); err != nil {
		return err
	}
	if human {
		if user, ok := body["user"].(map[string]any); ok {
			printAlbumUserTable(w, []contentMap{user})
			return nil
		}
	}
	return printJSON(w, body)
}

func printAlbumTable(w io.Writer, albums []contentMap) {
	rows := make([][]string, 0, len(albums))
	for _, album := range albums {
		rows = append(rows, []string{
			stringValue(album["id"]),
			stringValue(album["account_id"]),
			stringValue(album["archived"]),
			stringValue(album["content_count"]),
			stringValue(album["user_count"]),
			compact(stringValue(album["name"]), 60),
		})
	}
	printRows(w, []string{"ID", "ACCOUNT", "ARCHIVED", "CONTENTS", "USERS", "NAME"}, rows)
}

func printAlbumUserTable(w io.Writer, users []contentMap) {
	rows := make([][]string, 0, len(users))
	for _, user := range users {
		rows = append(rows, []string{
			stringValue(user["id"]),
			compact(stringValue(user["email"]), 48),
			compact(firstString(user, "name", "first_name", "last_name"), 40),
		})
	}
	printRows(w, []string{"ID", "EMAIL", "NAME"}, rows)
}
