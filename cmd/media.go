package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	dl "github.com/theinventor/footagepal-cli/internal/download"
	"github.com/theinventor/footagepal-cli/internal/exitcode"
)

type mediaSearchFlags struct {
	AccountID   string
	AlbumID     string
	Start       string
	End         string
	Date        string
	HasGPS      bool
	Near        string
	Lat         string
	Lng         string
	RadiusMiles string
	RadiusKM    string
	Filename    string
	Folder      string
	Tags        []string
	TagMatch    string
	Query       string
	MediaType   string
	Sort        string
	Direction   string
	Page        int
	PerPage     int
	Limit       int
}

type contentMap map[string]any

type searchResponse struct {
	Contents   []contentMap   `json:"contents"`
	Pagination map[string]any `json:"pagination,omitempty"`
	Links      map[string]any `json:"links,omitempty"`
}

type downloadEnvelope struct {
	Download downloadInfo `json:"download"`
}

type downloadInfo struct {
	ID               any    `json:"id"`
	URL              string `json:"url"`
	ExpiresAt        string `json:"expires_at"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
	Filename         string `json:"filename"`
	ContentType      string `json:"content_type"`
}

type downloadResult struct {
	ID               string `json:"id"`
	Filename         string `json:"filename"`
	Path             string `json:"path,omitempty"`
	Status           string `json:"status"`
	Bytes            int64  `json:"bytes,omitempty"`
	Error            string `json:"error,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
	ExpiresInSeconds int    `json:"expires_in_seconds,omitempty"`
}

func newMediaCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "media",
		Short: "Search, inspect, and download FootagePal media",
	}
	c.AddCommand(newMediaSearchCmd())
	c.AddCommand(newMediaGetCmd())
	c.AddCommand(newMediaDownloadCmd())
	c.AddCommand(newMediaUploadCmd())
	c.AddCommand(newMediaShareURLCmd())
	return c
}

func newMediaSearchCmd() *cobra.Command {
	var flags mediaSearchFlags
	var human bool
	c := &cobra.Command{
		Use:   "search",
		Short: "Search media by timestamp, GPS, text, tags, filename, folder, or type",
		RunE: func(cmd *cobra.Command, _ []string) error {
			query, err := buildSearchQuery(cmd, flags)
			if err != nil {
				return err
			}
			result, err := fetchSearch(context.Background(), newAPIClient(), query, flags.Limit)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if human {
				printContentTable(out, result.Contents)
				return nil
			}
			return printJSON(out, result)
		},
	}
	addSearchFlags(c, &flags, true)
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newMediaGetCmd() *cobra.Command {
	var accountID string
	var human bool
	c := &cobra.Command{
		Use:   "get <id>",
		Short: "Fetch one media record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if accountID != "" {
				q.Set("account_id", accountID)
			}
			cli := newAPIClient()
			resp, err := cli.Do(http.MethodGet, "/api/v1/contents/"+url.PathEscape(args[0]), nil, q)
			if err != nil {
				return err
			}
			var body map[string]any
			if err := decodeAPIResponse(resp, &body); err != nil {
				return err
			}
			if human {
				if content, ok := body["content"].(map[string]any); ok {
					printContentTable(cmd.OutOrStdout(), []contentMap{content})
					return nil
				}
			}
			return printJSON(cmd.OutOrStdout(), body)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id to query")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func newMediaDownloadCmd() *cobra.Command {
	var flags mediaSearchFlags
	var outDir, collision string
	var dryRun, yes, human, preserveFolders bool
	var retries int
	c := &cobra.Command{
		Use:   "download [id...]",
		Short: "Download one or many authorized original media files",
		Long: `Download media originals to an explicit output directory.

Pass one or more ids to download exact media records. With no ids, the command
runs the media search filters first; search-driven downloads require --dry-run
or --yes so large downloads are deliberate.`,
		RunE: func(cmd *cobra.Command, ids []string) error {
			if outDir == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--out is required"))
			}
			if collision != string(dl.CollisionSkip) && collision != string(dl.CollisionRename) && collision != string(dl.CollisionOverwrite) {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--collision must be one of skip, rename, overwrite"))
			}
			if len(ids) == 0 && !dryRun && !yes {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("search-driven downloads require --dry-run or --yes"))
			}
			if retries < 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--retries must be >= 0"))
			}
			cli := newAPIClient()
			items, err := downloadItems(cmd, cli, ids, flags)
			if err != nil {
				return err
			}
			results := make([]downloadResult, 0, len(items))
			for _, item := range items {
				result := downloadOne(cmd.Context(), cli, item, outDir, flags.AccountID, dl.CollisionPolicy(collision), preserveFolders, dryRun, retries)
				results = append(results, result)
			}
			summary := summarizeDownloads(results)
			payload := map[string]any{
				"output_dir": filepath.Clean(outDir),
				"dry_run":    dryRun,
				"summary":    summary,
				"results":    results,
			}
			if human {
				printDownloadTable(cmd.OutOrStdout(), results)
				return nil
			}
			return printJSON(cmd.OutOrStdout(), payload)
		},
	}
	addSearchFlags(c, &flags, true)
	c.Flags().StringVar(&outDir, "out", "", "output directory for downloaded files (required)")
	c.Flags().StringVar(&collision, "collision", string(dl.CollisionSkip), "collision policy: skip, rename, or overwrite")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "plan downloads without fetching signed URLs to disk")
	c.Flags().BoolVar(&yes, "yes", false, "confirm search-driven downloads without an interactive prompt")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	c.Flags().BoolVar(&preserveFolders, "preserve-folders", false, "preserve sanitized API folder paths for search-driven downloads")
	c.Flags().IntVar(&retries, "retries", 2, "retry count for signed URL downloads")
	return c
}

func addSearchFlags(c *cobra.Command, flags *mediaSearchFlags, includeAlbumID bool) {
	c.Flags().StringVar(&flags.AccountID, "account-id", "", "FootagePal account id")
	if includeAlbumID {
		c.Flags().StringVar(&flags.AlbumID, "album-id", "", "restrict results to a visible album id")
	}
	c.Flags().StringVar(&flags.Start, "start", "", "recorded_at start timestamp or date")
	c.Flags().StringVar(&flags.End, "end", "", "recorded_at end timestamp or date")
	c.Flags().StringVar(&flags.Date, "date", "", "exact recorded_at date (YYYY-MM-DD)")
	c.Flags().BoolVar(&flags.HasGPS, "has-gps", false, "filter by GPS presence; use --has-gps=false for records without GPS")
	c.Flags().StringVar(&flags.Near, "near", "", "GPS center as lat,lng")
	c.Flags().StringVar(&flags.Lat, "lat", "", "GPS latitude for nearby search")
	c.Flags().StringVar(&flags.Lng, "lng", "", "GPS longitude for nearby search")
	c.Flags().StringVar(&flags.RadiusMiles, "radius-miles", "", "nearby search radius in miles")
	c.Flags().StringVar(&flags.RadiusKM, "radius-km", "", "nearby search radius in kilometers")
	c.Flags().StringVar(&flags.Filename, "filename", "", "case-insensitive filename match")
	c.Flags().StringVar(&flags.Folder, "folder", "", "case-insensitive folder/blob prefix")
	c.Flags().StringSliceVar(&flags.Tags, "tag", nil, "tag filter; repeat or comma-separate")
	c.Flags().StringVar(&flags.TagMatch, "tag-match", "", "tag match mode: all or any")
	c.Flags().StringVar(&flags.Query, "query", "", "text search across name, path, caption, and transcription")
	c.Flags().StringVar(&flags.MediaType, "media-type", "", "media type: photo or video")
	c.Flags().StringVar(&flags.Sort, "sort", "", "sort field")
	c.Flags().StringVar(&flags.Direction, "direction", "", "sort direction: asc or desc")
	c.Flags().IntVar(&flags.Page, "page", 0, "1-based page number")
	c.Flags().IntVar(&flags.PerPage, "per-page", 0, "page size, max 100")
	c.Flags().IntVar(&flags.Limit, "limit", 0, "client-side max records to return or download")
}

func buildSearchQuery(cmd *cobra.Command, flags mediaSearchFlags) (url.Values, error) {
	q := url.Values{}
	set := func(key, value string) {
		if value != "" {
			q.Set(key, value)
		}
	}
	set("account_id", flags.AccountID)
	set("album_id", flags.AlbumID)
	set("start", flags.Start)
	set("end", flags.End)
	set("date", flags.Date)
	if cmd.Flags().Changed("has-gps") {
		q.Set("has_gps", strconv.FormatBool(flags.HasGPS))
	}
	set("near", flags.Near)
	set("lat", flags.Lat)
	set("lng", flags.Lng)
	set("radius_miles", flags.RadiusMiles)
	set("radius_km", flags.RadiusKM)
	set("filename", flags.Filename)
	set("folder", flags.Folder)
	if len(flags.Tags) > 0 {
		q.Set("tags", strings.Join(flags.Tags, ","))
	}
	set("tag_match", flags.TagMatch)
	set("q", flags.Query)
	set("media_type", flags.MediaType)
	set("sort", flags.Sort)
	set("direction", flags.Direction)
	if flags.Page < 0 || flags.PerPage < 0 || flags.Limit < 0 {
		return nil, exitcode.Wrap(exitcode.Usage, fmt.Errorf("--page, --per-page, and --limit must be >= 0"))
	}
	if flags.Page > 0 {
		q.Set("page", strconv.Itoa(flags.Page))
	}
	if flags.PerPage > 0 {
		q.Set("per_page", strconv.Itoa(flags.PerPage))
	} else if flags.Limit > 0 && flags.Limit < 100 {
		q.Set("per_page", strconv.Itoa(flags.Limit))
	}
	return q, nil
}

func fetchSearch(ctx context.Context, cli doer, q url.Values, limit int) (searchResponse, error) {
	if limit <= 0 {
		resp, err := cli.Do(http.MethodGet, "/api/v1/contents", nil, q)
		if err != nil {
			return searchResponse{}, err
		}
		var result searchResponse
		if err := decodeAPIResponse(resp, &result); err != nil {
			return searchResponse{}, err
		}
		return result, nil
	}

	page := intFromValues(q, "page", 1)
	perPage := intFromValues(q, "per_page", 100)
	if perPage <= 0 || perPage > 100 {
		perPage = 100
	}
	out := searchResponse{Contents: []contentMap{}}
	for len(out.Contents) < limit {
		select {
		case <-ctx.Done():
			return searchResponse{}, ctx.Err()
		default:
		}
		pageQuery := cloneValues(q)
		pageQuery.Set("page", strconv.Itoa(page))
		pageQuery.Set("per_page", strconv.Itoa(perPage))
		resp, err := cli.Do(http.MethodGet, "/api/v1/contents", nil, pageQuery)
		if err != nil {
			return searchResponse{}, err
		}
		var result searchResponse
		if err := decodeAPIResponse(resp, &result); err != nil {
			return searchResponse{}, err
		}
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

type doer interface {
	Do(method, path string, body any, query url.Values) (*http.Response, error)
}

func intFromValues(q url.Values, key string, fallback int) int {
	if v := q.Get(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func cloneValues(q url.Values) url.Values {
	clone := url.Values{}
	for k, vals := range q {
		clone[k] = append([]string(nil), vals...)
	}
	return clone
}

func hasNextPage(p map[string]any, current int) bool {
	if p == nil {
		return false
	}
	totalPages := intFromAny(p["total_pages"])
	if totalPages == 0 {
		return false
	}
	return current < totalPages
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		i, _ := x.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(x)
		return i
	default:
		return 0
	}
}

type downloadItem struct {
	ID       string
	Filename string
	Folder   string
}

func downloadItems(cmd *cobra.Command, cli doer, ids []string, flags mediaSearchFlags) ([]downloadItem, error) {
	if len(ids) > 0 {
		items := make([]downloadItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, downloadItem{ID: id})
		}
		return items, nil
	}
	q, err := buildSearchQuery(cmd, flags)
	if err != nil {
		return nil, err
	}
	result, err := fetchSearch(cmd.Context(), cli, q, flags.Limit)
	if err != nil {
		return nil, err
	}
	items := make([]downloadItem, 0, len(result.Contents))
	for _, content := range result.Contents {
		id := stringValue(content["id"])
		if id == "" {
			continue
		}
		items = append(items, downloadItem{
			ID:       id,
			Filename: firstString(content, "file_name", "pretty_name", "display_name", "name"),
			Folder:   stringValue(content["folder"]),
		})
	}
	return items, nil
}

func downloadOne(ctx context.Context, cli doer, item downloadItem, outDir, accountID string, collision dl.CollisionPolicy, preserveFolders, dryRun bool, retries int) downloadResult {
	result := downloadResult{ID: item.ID, Status: "failed"}
	q := url.Values{}
	if accountID != "" {
		q.Set("account_id", accountID)
	}
	resp, err := cli.Do(http.MethodGet, "/api/v1/contents/"+url.PathEscape(item.ID)+"/download", nil, q)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	var envelope downloadEnvelope
	if err := decodeAPIResponse(resp, &envelope); err != nil {
		result.Error = err.Error()
		return result
	}
	info := envelope.Download
	filename := info.Filename
	if filename == "" {
		filename = item.Filename
	}
	result.Filename = filename
	result.ExpiresAt = info.ExpiresAt
	result.ExpiresInSeconds = info.ExpiresInSeconds
	target, err := dl.ResolveTarget(outDir, item.ID, filename, item.Folder, preserveFolders, collision)
	if err != nil {
		if strings.Contains(err.Error(), "target exists") && collision == dl.CollisionSkip {
			result.Status = "skipped"
			result.Path = filepath.Clean(filepath.Join(outDir, dl.SafeFilename(item.ID+"_"+filename)))
			return result
		}
		result.Error = err.Error()
		return result
	}
	result.Path = target.Path
	result.Status = target.Status
	if dryRun {
		result.Status = "planned"
		return result
	}
	httpClient := &http.Client{Timeout: 2 * time.Minute}
	fetch, err := dl.FetchURL(ctx, httpClient, info.URL, target.Path, dl.FetchOptions{Retries: retries})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Bytes = fetch.Bytes
	if result.Status == "planned" {
		result.Status = "downloaded"
	}
	return result
}

func summarizeDownloads(results []downloadResult) map[string]int {
	summary := map[string]int{
		"total":       len(results),
		"planned":     0,
		"downloaded":  0,
		"skipped":     0,
		"renamed":     0,
		"overwritten": 0,
		"failed":      0,
	}
	for _, result := range results {
		if result.Error != "" {
			summary["failed"]++
			continue
		}
		if _, ok := summary[result.Status]; ok {
			summary[result.Status]++
		}
	}
	return summary
}

func printContentTable(w io.Writer, contents []contentMap) {
	rows := make([][]string, 0, len(contents))
	for _, c := range contents {
		gps := ""
		if stringValue(c["has_gps"]) == "true" {
			gps = strings.Trim(strings.Join([]string{stringValue(c["latitude"]), stringValue(c["longitude"])}, ","), ",")
		}
		rows = append(rows, []string{
			stringValue(c["id"]),
			stringValue(c["media_type"]),
			stringValue(c["recorded_at"]),
			gps,
			compact(firstString(c, "file_name", "display_name", "name"), 60),
		})
	}
	printRows(w, []string{"ID", "TYPE", "RECORDED_AT", "GPS", "FILE"}, rows)
}

func printDownloadTable(w io.Writer, results []downloadResult) {
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{r.ID, r.Status, strconv.FormatInt(r.Bytes, 10), compact(r.Path, 80), compact(r.Error, 80)})
	}
	printRows(w, []string{"ID", "STATUS", "BYTES", "PATH", "ERROR"}, rows)
}

func firstString(m contentMap, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(m[key]); value != "" {
			return value
		}
	}
	return ""
}
