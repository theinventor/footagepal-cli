package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	clientpkg "github.com/theinventor/footagepal-cli/internal/client"
	"github.com/theinventor/footagepal-cli/internal/exitcode"
)

const (
	defaultUploadContentType = "application/octet-stream"
	largeUploadThreshold     = 100 * 1024 * 1024
)

type mediaUploadFlags struct {
	AccountID       string
	AlbumID         string
	UserID          string
	StorageBucketID string
	Name            string
	ContentType     string
	Tags            []string
	Metadata        []string
	MetadataJSON    string
	DryRun          bool
	Yes             bool
	Human           bool
	Retries         int
}

type uploadPlan struct {
	Path        string         `json:"path"`
	Filename    string         `json:"filename"`
	Name        string         `json:"name,omitempty"`
	ByteSize    int64          `json:"byte_size"`
	ContentType string         `json:"content_type"`
	Tags        []string       `json:"tags,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type uploadEnvelope struct {
	Upload uploadPreflight `json:"upload"`
}

type uploadPreflight struct {
	Method           string            `json:"method"`
	URL              string            `json:"url"`
	ExpiresAt        string            `json:"expires_at"`
	ExpiresInSeconds int               `json:"expires_in_seconds"`
	BlobName         string            `json:"blob_name"`
	StorageBucketID  any               `json:"storage_bucket_id"`
	AccountID        any               `json:"account_id"`
	AlbumID          any               `json:"album_id"`
	UserID           any               `json:"user_id"`
	Filename         string            `json:"filename"`
	ByteSize         int64             `json:"byte_size"`
	ContentType      string            `json:"content_type"`
	Headers          map[string]string `json:"headers"`
	Completion       uploadCompletion  `json:"completion"`
}

type uploadCompletion struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type uploadResult struct {
	File             string      `json:"file"`
	Filename         string      `json:"filename"`
	Status           string      `json:"status"`
	Bytes            int64       `json:"bytes,omitempty"`
	ContentID        string      `json:"content_id,omitempty"`
	BlobName         string      `json:"blob_name,omitempty"`
	ExpiresAt        string      `json:"expires_at,omitempty"`
	ExpiresInSeconds int         `json:"expires_in_seconds,omitempty"`
	Error            string      `json:"error,omitempty"`
	Planned          *uploadPlan `json:"planned,omitempty"`
}

var sensitiveURLPattern = regexp.MustCompile(`https?://[^\s"'<>)]+`)

func newMediaUploadCmd() *cobra.Command {
	var flags mediaUploadFlags
	c := &cobra.Command{
		Use:   "upload <file...>",
		Short: "Upload one or many local media files through the FootagePal API",
		Long: `Upload local photo or video files through FootagePal's safe handoff API.

The CLI sends upload preflight metadata to FootagePal, streams bytes to the
short-lived signed storage URL returned by the API, then completes the upload
with FootagePal. Signed upload URLs are never printed or persisted.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.AccountID) == "" {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--account-id is required for uploads"))
			}
			if flags.Retries < 0 {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("--retries must be >= 0"))
			}
			metadata, err := parseUploadMetadata(flags)
			if err != nil {
				return err
			}
			plans, totalBytes, err := buildUploadPlans(args, flags, metadata)
			if err != nil {
				return err
			}
			if uploadNeedsConfirmation(plans, totalBytes) && !flags.DryRun && !flags.Yes {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("bulk or large uploads require --dry-run or --yes"))
			}
			results := make([]uploadResult, 0, len(plans))
			if flags.DryRun {
				for _, plan := range plans {
					planned := plan
					results = append(results, uploadResult{
						File:     plan.Path,
						Filename: plan.Filename,
						Status:   "planned",
						Bytes:    plan.ByteSize,
						Planned:  &planned,
					})
				}
				return printUploadOutput(cmd.OutOrStdout(), flags.Human, true, flags.AccountID, flags.AlbumID, totalBytes, results)
			}

			cli := newAPIClient()
			uploadHTTPClient := &http.Client{Timeout: 10 * time.Minute}
			for _, plan := range plans {
				results = append(results, uploadOneMedia(cmd.Context(), cli, uploadHTTPClient, plan, flags, metadata))
			}
			if err := printUploadOutput(cmd.OutOrStdout(), flags.Human, false, flags.AccountID, flags.AlbumID, totalBytes, results); err != nil {
				return err
			}
			if failed := summarizeUploads(results)["failed"]; failed > 0 {
				return exitcode.Wrap(exitcode.Generic, fmt.Errorf("%d upload(s) failed", failed))
			}
			return nil
		},
	}
	c.Flags().StringVar(&flags.AccountID, "account-id", "", "FootagePal account id (required)")
	c.Flags().StringVar(&flags.AlbumID, "album-id", "", "album id to attach completed uploads to")
	c.Flags().StringVar(&flags.UserID, "user-id", "", "attribute uploads to this account-local user id when authorized")
	c.Flags().StringVar(&flags.StorageBucketID, "storage-bucket-id", "", "storage bucket id to use when authorized")
	c.Flags().StringVar(&flags.Name, "name", "", "content display name; only valid with one file")
	c.Flags().StringVar(&flags.ContentType, "content-type", "", "content type override; defaults from file extension or application/octet-stream")
	c.Flags().StringSliceVar(&flags.Tags, "tag", nil, "tag to add on completion; repeat or comma-separate")
	c.Flags().StringArrayVar(&flags.Metadata, "metadata", nil, "metadata key=value pair to merge into upload metadata")
	c.Flags().StringVar(&flags.MetadataJSON, "metadata-json", "", "metadata object as JSON")
	c.Flags().BoolVar(&flags.DryRun, "dry-run", false, "list planned uploads without preflight, signed URL requests, or writes")
	c.Flags().BoolVar(&flags.Yes, "yes", false, "confirm bulk or large uploads")
	c.Flags().BoolVar(&flags.Human, "human", false, "render a compact human table instead of JSON")
	c.Flags().IntVar(&flags.Retries, "retries", 2, "retry count for direct upload and completion requests")
	return c
}

func newMediaShareURLCmd() *cobra.Command {
	var accountID string
	var yes, dryRun, human bool
	c := &cobra.Command{
		Use:     "share-url <content-id>",
		Aliases: []string{"share"},
		Short:   "Create an authorized public share URL for media",
		Long: `Create a public FootagePal share URL for one media record.

Share URLs use public-link semantics. Anyone with the URL can view or download
the content until the server-side share-key scheme changes.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dryRun && !yes {
				return exitcode.Wrap(exitcode.Usage, fmt.Errorf("creating a public share URL requires --yes or --dry-run"))
			}
			if dryRun {
				payload := map[string]any{
					"dry_run":    true,
					"content_id": args[0],
					"account_id": emptyStringAsNil(accountID),
					"status":     "planned",
					"public":     true,
					"warning":    "Share URLs are public links; no API request was made.",
				}
				if human {
					printRows(cmd.OutOrStdout(), []string{"ID", "STATUS", "PUBLIC", "WARNING"}, [][]string{{args[0], "planned", "true", "no API request made"}})
					return nil
				}
				return printJSON(cmd.OutOrStdout(), payload)
			}
			q := accountQuery(accountID)
			resp, err := newAPIClient().Do(http.MethodPost, "/api/v1/contents/"+url.PathEscape(args[0])+"/share", nil, q)
			if err != nil {
				return err
			}
			var body map[string]any
			if err := decodeAPIResponse(resp, &body); err != nil {
				return err
			}
			if human {
				if share, ok := body["share"].(map[string]any); ok {
					printShareTable(cmd.OutOrStdout(), []contentMap{share})
					return nil
				}
			}
			return printJSON(cmd.OutOrStdout(), body)
		},
	}
	c.Flags().StringVar(&accountID, "account-id", "", "FootagePal account id")
	c.Flags().BoolVar(&yes, "yes", false, "confirm creating and printing a public share URL")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "show planned public share creation without calling the API")
	c.Flags().BoolVar(&human, "human", false, "render a compact human table instead of JSON")
	return c
}

func buildUploadPlans(paths []string, flags mediaUploadFlags, metadata map[string]any) ([]uploadPlan, int64, error) {
	if flags.Name != "" && len(paths) > 1 {
		return nil, 0, exitcode.Wrap(exitcode.Usage, fmt.Errorf("--name can only be used with one file"))
	}
	plans := make([]uploadPlan, 0, len(paths))
	var total int64
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, 0, exitcode.Wrap(exitcode.Validation, fmt.Errorf("stat upload file %s: %w", path, err))
		}
		if info.IsDir() {
			return nil, 0, exitcode.Wrap(exitcode.Usage, fmt.Errorf("upload paths must be files, not directories: %s", path))
		}
		if info.Size() <= 0 {
			return nil, 0, exitcode.Wrap(exitcode.Validation, fmt.Errorf("upload file must be non-empty: %s", path))
		}
		filename := filepath.Base(path)
		contentType := strings.TrimSpace(flags.ContentType)
		if contentType == "" {
			contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
		}
		if contentType == "" {
			contentType = defaultUploadContentType
		}
		plan := uploadPlan{
			Path:        filepath.Clean(path),
			Filename:    filename,
			Name:        flags.Name,
			ByteSize:    info.Size(),
			ContentType: contentType,
			Tags:        append([]string(nil), flags.Tags...),
			Metadata:    cloneMetadata(metadata),
		}
		plans = append(plans, plan)
		total += info.Size()
	}
	return plans, total, nil
}

func uploadNeedsConfirmation(plans []uploadPlan, totalBytes int64) bool {
	return len(plans) > 1 || totalBytes > largeUploadThreshold
}

func parseUploadMetadata(flags mediaUploadFlags) (map[string]any, error) {
	metadata := map[string]any{}
	if strings.TrimSpace(flags.MetadataJSON) != "" {
		dec := json.NewDecoder(strings.NewReader(flags.MetadataJSON))
		dec.UseNumber()
		if err := dec.Decode(&metadata); err != nil {
			return nil, exitcode.Wrap(exitcode.Usage, fmt.Errorf("parse --metadata-json: %w", err))
		}
		if metadata == nil {
			metadata = map[string]any{}
		}
	}
	for _, pair := range flags.Metadata {
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, exitcode.Wrap(exitcode.Usage, fmt.Errorf("--metadata values must be key=value"))
		}
		metadata[key] = value
	}
	return metadata, nil
}

func uploadOneMedia(ctx context.Context, cli doer, httpClient *http.Client, plan uploadPlan, flags mediaUploadFlags, metadata map[string]any) uploadResult {
	result := uploadResult{
		File:     plan.Path,
		Filename: plan.Filename,
		Status:   "failed",
	}
	preflightBody := uploadRequestBody(flags, plan, metadata)
	var preflight uploadEnvelope
	if err := doJSONWithRetry(cli, http.MethodPost, "/api/v1/uploads", preflightBody, nil, flags.Retries, &preflight); err != nil {
		result.Error = redactSensitiveText(err.Error())
		return result
	}
	if preflight.Upload.URL == "" || preflight.Upload.BlobName == "" {
		result.Error = "upload preflight response missing signed URL or blob_name"
		return result
	}
	method := preflight.Upload.Method
	if method == "" {
		method = http.MethodPut
	}
	headers := uploadHeadersWithFallback(preflight.Upload.Headers, plan.ContentType)
	bytesUploaded, err := putUploadFileWithRetries(ctx, httpClient, method, preflight.Upload.URL, plan.Path, headers, flags.Retries)
	if err != nil {
		result.Error = redactSensitiveText(err.Error())
		return result
	}
	completionPath := preflight.Upload.Completion.Path
	if completionPath == "" {
		completionPath = "/api/v1/uploads/complete"
	}
	completionMethod := preflight.Upload.Completion.Method
	if completionMethod == "" {
		completionMethod = http.MethodPost
	}
	completionBody := uploadCompletionBody(flags, plan, metadata, preflight.Upload)
	var completed map[string]any
	if err := doJSONWithRetry(cli, completionMethod, completionPath, completionBody, nil, flags.Retries, &completed); err != nil {
		result.Error = redactSensitiveText(err.Error())
		result.Bytes = bytesUploaded
		result.BlobName = preflight.Upload.BlobName
		return result
	}
	result.Status = "completed"
	result.Bytes = bytesUploaded
	result.BlobName = preflight.Upload.BlobName
	result.ExpiresAt = preflight.Upload.ExpiresAt
	result.ExpiresInSeconds = preflight.Upload.ExpiresInSeconds
	if content, ok := completed["content"].(map[string]any); ok {
		result.ContentID = stringValue(content["id"])
	}
	return result
}

func uploadRequestBody(flags mediaUploadFlags, plan uploadPlan, metadata map[string]any) map[string]any {
	body := map[string]any{
		"account_id":   flags.AccountID,
		"filename":     plan.Filename,
		"byte_size":    plan.ByteSize,
		"content_type": plan.ContentType,
	}
	addUploadOptionalFields(body, flags, plan, metadata)
	return body
}

func uploadCompletionBody(flags mediaUploadFlags, plan uploadPlan, metadata map[string]any, preflight uploadPreflight) map[string]any {
	filename := preflight.Filename
	if filename == "" {
		filename = plan.Filename
	}
	byteSize := preflight.ByteSize
	if byteSize <= 0 {
		byteSize = plan.ByteSize
	}
	contentType := preflight.ContentType
	if contentType == "" {
		contentType = plan.ContentType
	}
	body := map[string]any{
		"account_id":        flags.AccountID,
		"storage_bucket_id": preflight.StorageBucketID,
		"blob_name":         preflight.BlobName,
		"filename":          filename,
		"byte_size":         byteSize,
		"content_type":      contentType,
	}
	addUploadOptionalFields(body, flags, plan, metadata)
	return body
}

func addUploadOptionalFields(body map[string]any, flags mediaUploadFlags, plan uploadPlan, metadata map[string]any) {
	if flags.AlbumID != "" {
		body["album_id"] = flags.AlbumID
	}
	if flags.UserID != "" {
		body["user_id"] = flags.UserID
	}
	if flags.StorageBucketID != "" && body["storage_bucket_id"] == nil {
		body["storage_bucket_id"] = flags.StorageBucketID
	}
	if plan.Name != "" {
		body["name"] = plan.Name
	}
	if len(flags.Tags) > 0 {
		body["tags"] = append([]string(nil), flags.Tags...)
	}
	if len(metadata) > 0 {
		body["metadata"] = cloneMetadata(metadata)
	}
}

func doJSONWithRetry(cli doer, method, path string, body any, query url.Values, retries int, out any) error {
	attempts := retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := cli.Do(method, path, body, query)
		if err != nil {
			lastErr = err
		} else if resp.StatusCode >= 500 && attempt < attempts {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, path)
		} else {
			return decodeAPIResponse(resp, out)
		}
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	}
	return lastErr
}

func putUploadFileWithRetries(ctx context.Context, httpClient *http.Client, method, rawURL, path string, headers map[string]string, retries int) (int64, error) {
	if strings.ToUpper(method) != http.MethodPut {
		return 0, fmt.Errorf("unsupported upload method %q", method)
	}
	attempts := retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		n, err := putUploadFileOnce(ctx, httpClient, rawURL, path, headers)
		if err == nil {
			return n, nil
		}
		lastErr = err
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	}
	return 0, lastErr
}

func putUploadFileOnce(ctx context.Context, httpClient *http.Client, rawURL, path string, headers map[string]string) (int64, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, rawURL, file)
	if err != nil {
		return 0, fmt.Errorf("build signed upload request %s: %w", clientpkg.RedactURL(rawURL), err)
	}
	req.ContentLength = info.Size()
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("upload signed URL %s: %s", clientpkg.RedactURL(rawURL), redactSensitiveText(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		detail := strings.TrimSpace(string(body))
		if detail != "" {
			return 0, fmt.Errorf("upload signed URL %s: HTTP %d: %s", clientpkg.RedactURL(rawURL), resp.StatusCode, redactSensitiveText(detail))
		}
		return 0, fmt.Errorf("upload signed URL %s: HTTP %d", clientpkg.RedactURL(rawURL), resp.StatusCode)
	}
	return info.Size(), nil
}

func uploadHeadersWithFallback(headers map[string]string, contentType string) map[string]string {
	out := make(map[string]string, len(headers)+1)
	hasContentType := false
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			hasContentType = true
		}
		out[key] = value
	}
	if !hasContentType && contentType != "" {
		out["Content-Type"] = contentType
	}
	return out
}

func printUploadOutput(w io.Writer, human, dryRun bool, accountID, albumID string, totalBytes int64, results []uploadResult) error {
	payload := map[string]any{
		"dry_run":               dryRun,
		"account_id":            accountID,
		"album_id":              emptyStringAsNil(albumID),
		"total_bytes":           totalBytes,
		"large_threshold_bytes": largeUploadThreshold,
		"summary":               summarizeUploads(results),
		"results":               results,
	}
	if human {
		printUploadTable(w, results)
		return nil
	}
	return printJSON(w, payload)
}

func summarizeUploads(results []uploadResult) map[string]int {
	summary := map[string]int{
		"total":     len(results),
		"planned":   0,
		"completed": 0,
		"failed":    0,
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

func printUploadTable(w io.Writer, results []uploadResult) {
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{
			r.Status,
			strconv.FormatInt(r.Bytes, 10),
			r.ContentID,
			compact(r.Filename, 48),
			compact(r.Error, 80),
		})
	}
	printRows(w, []string{"STATUS", "BYTES", "CONTENT_ID", "FILE", "ERROR"}, rows)
}

func printShareTable(w io.Writer, shares []contentMap) {
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		rows = append(rows, []string{
			stringValue(share["id"]),
			stringValue(share["access"]),
			stringValue(share["public"]),
			compact(stringValue(share["url"]), 64),
			compact(stringValue(share["warning"]), 80),
		})
	}
	printRows(w, []string{"ID", "ACCESS", "PUBLIC", "URL", "WARNING"}, rows)
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	clone := make(map[string]any, len(metadata))
	for key, value := range metadata {
		clone[key] = value
	}
	return clone
}

func emptyStringAsNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func redactSensitiveText(text string) string {
	if text == "" {
		return text
	}
	return sensitiveURLPattern.ReplaceAllStringFunc(text, func(match string) string {
		trailing := ""
		for len(match) > 0 {
			last := match[len(match)-1]
			if last != '.' && last != ',' && last != ';' && last != ':' {
				break
			}
			trailing = string(last) + trailing
			match = match[:len(match)-1]
		}
		return clientpkg.RedactURL(match) + trailing
	})
}
