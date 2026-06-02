package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theinventor/footagepal-cli/internal/client"
	"github.com/theinventor/footagepal-cli/internal/config"
)

type captured struct {
	method     string
	path       string
	rawQuery   string
	authHeader string
	hits       int
	requests   []capturedRequest
}

type capturedRequest struct {
	method     string
	path       string
	rawQuery   string
	authHeader string
	body       string
	headers    http.Header
}

func runRoot(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "footagepal: %v\n", err)
	}
	return stdout.String(), stderr.String(), err
}

func runWithAPIServer(t *testing.T, args []string, handler http.HandlerFunc) (string, string, *captured, error) {
	t.Helper()
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		cap.hits++
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.rawQuery = r.URL.RawQuery
		cap.authHeader = r.Header.Get("Authorization")
		cap.requests = append(cap.requests, capturedRequest{
			method:     r.Method,
			path:       r.URL.Path,
			rawQuery:   r.URL.RawQuery,
			authHeader: r.Header.Get("Authorization"),
			body:       string(body),
			headers:    r.Header.Clone(),
		})
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(client.EnvAPIURL, srv.URL)
	t.Setenv(client.EnvAPIToken, "fp_test_secret_123456789")
	returnedStdout, returnedStderr, err := runRoot(t, args...)
	return returnedStdout, returnedStderr, cap, err
}

func TestAuthSaveAndStatusNeverPrintRawToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(config.EnvConfig, configPath)
	token := "fp_super_secret_token_123456789"

	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.hits++
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.authHeader = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"user":{"id":1}}`)
	}))
	defer srv.Close()

	stdout, stderr, err := runRoot(t, "auth", "save", "--profile", "main", "--token", token, "--api-url", srv.URL, "--storage", "file")
	if err != nil {
		t.Fatalf("auth save error: %v stderr=%s", err, stderr)
	}
	if strings.Contains(stdout, token) || strings.Contains(stderr, token) {
		t.Fatalf("auth save printed raw token stdout=%q stderr=%q", stdout, stderr)
	}

	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(rawConfig), token) {
		t.Fatalf("file-backed profile should store token in mode-0600 config for explicit --storage=file")
	}

	stdout, _, err = runRoot(t, "auth", "status")
	if err != nil {
		t.Fatalf("auth status error: %v", err)
	}
	if cap.authHeader != "token "+token {
		t.Fatalf("auth status Authorization = %q", cap.authHeader)
	}
	if strings.Contains(stdout, token) {
		t.Fatalf("auth status printed raw token: %q", stdout)
	}
}

func TestMediaSearchBuildsExpectedRequest(t *testing.T) {
	stdout, _, cap, err := runWithAPIServer(t, []string{
		"media", "search",
		"--account-id", "123",
		"--album-id", "77",
		"--start", "2024-03-01T00:00:00Z",
		"--end", "2024-03-31T23:59:59Z",
		"--has-gps",
		"--near", "47.6205,-122.3493",
		"--radius-miles", "10",
		"--filename", "DJI",
		"--folder", "2024/03",
		"--tag", "drone",
		"--tag", "sunset",
		"--tag-match", "any",
		"--query", "mountain",
		"--media-type", "video",
		"--sort", "recorded_at",
		"--direction", "asc",
		"--page", "2",
		"--per-page", "25",
	}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"contents":[],"pagination":{"page":2,"per_page":25,"total_count":0,"total_pages":0}}`)
	})
	if err != nil {
		t.Fatalf("media search error: %v stdout=%s", err, stdout)
	}
	if cap.method != http.MethodGet || cap.path != "/api/v1/contents" {
		t.Fatalf("request = %s %s", cap.method, cap.path)
	}
	if cap.authHeader != "token fp_test_secret_123456789" {
		t.Fatalf("Authorization = %q", cap.authHeader)
	}
	values := mustParseQuery(t, cap.rawQuery)
	want := map[string]string{
		"account_id":   "123",
		"album_id":     "77",
		"start":        "2024-03-01T00:00:00Z",
		"end":          "2024-03-31T23:59:59Z",
		"has_gps":      "true",
		"near":         "47.6205,-122.3493",
		"radius_miles": "10",
		"filename":     "DJI",
		"folder":       "2024/03",
		"tags":         "drone,sunset",
		"tag_match":    "any",
		"q":            "mountain",
		"media_type":   "video",
		"sort":         "recorded_at",
		"direction":    "asc",
		"page":         "2",
		"per_page":     "25",
	}
	for key, wantValue := range want {
		if got := values.Get(key); got != wantValue {
			t.Fatalf("query %s = %q, want %q (raw=%s)", key, got, wantValue, cap.rawQuery)
		}
	}
}

func TestMediaGetRepresentativeResponse(t *testing.T) {
	stdout, _, cap, err := runWithAPIServer(t, []string{"media", "get", "42", "--account-id", "123"}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":{"id":42,"name":"Drone Sunset","media_type":"video","metadata":{"summary":"Mountain sunset drone footage"},"transcription":{"text":"Drone rises."}}}`)
	})
	if err != nil {
		t.Fatalf("media get error: %v", err)
	}
	if cap.path != "/api/v1/contents/42" {
		t.Fatalf("path = %q", cap.path)
	}
	if values := mustParseQuery(t, cap.rawQuery); values.Get("account_id") != "123" {
		t.Fatalf("missing account_id query: %s", cap.rawQuery)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(stdout), &body); err != nil {
		t.Fatalf("stdout JSON: %v\n%s", err, stdout)
	}
	content := body["content"].(map[string]any)
	if content["name"] != "Drone Sunset" {
		t.Fatalf("content name = %v", content["name"])
	}
}

func TestSearchDrivenDownloadRequiresDryRunOrYes(t *testing.T) {
	stdout, stderr, cap, err := runWithAPIServer(t, []string{"media", "download", "--out", t.TempDir(), "--query", "mountain"}, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s", r.URL.String())
	})
	if err == nil {
		t.Fatalf("expected usage error stdout=%s stderr=%s", stdout, stderr)
	}
	if cap.hits != 0 {
		t.Fatalf("expected no HTTP calls, got %d", cap.hits)
	}
	if !strings.Contains(stderr, "require --dry-run or --yes") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestMediaDownloadFetchesFileAndRedactsSignedURL(t *testing.T) {
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "video-bytes")
	}))
	defer fileSrv.Close()
	signedURL := fileSrv.URL + "/DJI_0001.MP4?sig=SECRET"
	outDir := t.TempDir()

	stdout, stderr, _, err := runWithAPIServer(t, []string{"media", "download", "--out", outDir, "--yes", "--query", "mountain", "--limit", "1"}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/contents":
			_, _ = io.WriteString(w, `{"contents":[{"id":42,"file_name":"DJI_0001.MP4","folder":"2024/03"}],"pagination":{"page":1,"per_page":1,"total_count":1,"total_pages":1}}`)
		case "/api/v1/contents/42/download":
			_, _ = fmt.Fprintf(w, `{"download":{"id":42,"url":%q,"expires_at":"2026-06-01T12:15:00Z","expires_in_seconds":900,"filename":"DJI_0001.MP4","content_type":"video/mp4"}}`, signedURL)
		default:
			http.NotFound(w, r)
		}
	})
	if err != nil {
		t.Fatalf("media download error: %v stderr=%s stdout=%s", err, stderr, stdout)
	}
	if strings.Contains(stdout, "SECRET") || strings.Contains(stderr, "SECRET") || strings.Contains(stdout, signedURL) {
		t.Fatalf("signed URL leaked stdout=%q stderr=%q", stdout, stderr)
	}
	path := filepath.Join(outDir, "42_DJI_0001.MP4")
	if got, err := os.ReadFile(path); err != nil || string(got) != "video-bytes" {
		t.Fatalf("downloaded file = %q err=%v", string(got), err)
	}
	var body struct {
		Summary map[string]int `json:"summary"`
		Results []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Path   string `json:"path"`
			Bytes  int64  `json:"bytes"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &body); err != nil {
		t.Fatalf("stdout JSON: %v\n%s", err, stdout)
	}
	if body.Summary["downloaded"] != 1 || len(body.Results) != 1 || body.Results[0].Status != "downloaded" || body.Results[0].Bytes != int64(len("video-bytes")) {
		t.Fatalf("unexpected download summary/body: %#v", body)
	}
}

func TestAlbumsCreateUpdateContentsAndAccessBuildRequests(t *testing.T) {
	stdout, _, cap, err := runWithAPIServer(t, []string{"albums", "create", "--account-id", "123", "--name", "June Handoff"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/albums" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"album":{"id":77,"account_id":123,"name":"June Handoff","archived":false,"content_count":0,"user_count":0}}`)
	})
	if err != nil {
		t.Fatalf("albums create error: %v stdout=%s", err, stdout)
	}
	createBody := requestBodyMap(t, lastRequest(t, cap))
	if createBody["account_id"] != "123" {
		t.Fatalf("create account_id = %#v", createBody["account_id"])
	}
	if album := createBody["album"].(map[string]any); album["name"] != "June Handoff" {
		t.Fatalf("create album payload = %#v", album)
	}

	stdout, _, cap, err = runWithAPIServer(t, []string{"albums", "update", "77", "--account-id", "123", "--archived=false"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/albums/77" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"album":{"id":77,"account_id":123,"name":"June Handoff","archived":false,"content_count":0,"user_count":0}}`)
	})
	if err != nil {
		t.Fatalf("albums update error: %v stdout=%s", err, stdout)
	}
	updateBody := requestBodyMap(t, lastRequest(t, cap))
	if album := updateBody["album"].(map[string]any); album["archived"] != false {
		t.Fatalf("update album payload = %#v", album)
	}

	stdout, _, cap, err = runWithAPIServer(t, []string{"albums", "contents", "add", "77", "42", "--account-id", "123"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/albums/77/contents" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"album":{"id":77,"content_count":1},"content":{"id":42,"file_name":"clip.mp4"}}`)
	})
	if err != nil {
		t.Fatalf("albums contents add error: %v stdout=%s", err, stdout)
	}
	contentBody := requestBodyMap(t, lastRequest(t, cap))
	if contentBody["account_id"] != "123" || contentBody["content_id"] != "42" {
		t.Fatalf("content add payload = %#v", contentBody)
	}

	stdout, _, cap, err = runWithAPIServer(t, []string{"albums", "access", "add", "77", "--account-id", "123", "--email", "employee-b@example.com"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/albums/77/users" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"album":{"id":77,"user_count":1},"user":{"id":9,"email":"employee-b@example.com"}}`)
	})
	if err != nil {
		t.Fatalf("albums access add error: %v stdout=%s", err, stdout)
	}
	accessBody := requestBodyMap(t, lastRequest(t, cap))
	if accessBody["account_id"] != "123" || accessBody["email"] != "employee-b@example.com" {
		t.Fatalf("access add payload = %#v", accessBody)
	}

	stdout, _, cap, err = runWithAPIServer(t, []string{"albums", "access", "remove", "77", "9", "--account-id", "123"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/albums/77/users/9" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"album":{"id":77,"user_count":0},"user":{"id":9,"email":"employee-b@example.com"}}`)
	})
	if err != nil {
		t.Fatalf("albums access remove error: %v stdout=%s", err, stdout)
	}
	if values := mustParseQuery(t, cap.rawQuery); values.Get("account_id") != "123" {
		t.Fatalf("access remove query = %s", cap.rawQuery)
	}
}

func TestMediaUploadDryRunAndBulkConfirmationDoNotCallAPI(t *testing.T) {
	dir := t.TempDir()
	first := writeTestFile(t, dir, "first.mp4", "first-bytes")
	second := writeTestFile(t, dir, "second.mp4", "second-bytes")

	stdout, _, cap, err := runWithAPIServer(t, []string{"media", "upload", first, second, "--account-id", "123", "--dry-run"}, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s", r.URL.String())
	})
	if err != nil {
		t.Fatalf("upload dry-run error: %v stdout=%s", err, stdout)
	}
	if cap.hits != 0 {
		t.Fatalf("dry-run should not call API, got %d hits", cap.hits)
	}
	var dryRunBody map[string]any
	if err := json.Unmarshal([]byte(stdout), &dryRunBody); err != nil {
		t.Fatalf("dry-run JSON: %v\n%s", err, stdout)
	}
	if dryRunBody["dry_run"] != true {
		t.Fatalf("dry_run = %#v", dryRunBody["dry_run"])
	}

	stdout, stderr, cap, err := runWithAPIServer(t, []string{"media", "upload", first, second, "--account-id", "123"}, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s", r.URL.String())
	})
	if err == nil {
		t.Fatalf("expected bulk confirmation error stdout=%s stderr=%s", stdout, stderr)
	}
	if cap.hits != 0 {
		t.Fatalf("bulk confirmation failure should not call API, got %d hits", cap.hits)
	}
	if !strings.Contains(stderr, "require --dry-run or --yes") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestMediaUploadPreflightPutCompleteAndRedactsSignedURL(t *testing.T) {
	dir := t.TempDir()
	file := writeTestFile(t, dir, "employee-a-clip.mp4", "video-bytes")
	var putHits int
	var uploadedBody string
	var uploadContentType string

	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		putHits++
		uploadContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		uploadedBody = string(body)
		if putHits == 1 {
			http.Error(w, "temporary failure sig=SECRET", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer uploadSrv.Close()
	signedURL := uploadSrv.URL + "/fp-api/employee-a-clip.mp4?sig=SECRET"

	stdout, stderr, cap, err := runWithAPIServer(t, []string{
		"media", "upload", file,
		"--account-id", "123",
		"--album-id", "77",
		"--user-id", "5",
		"--content-type", "video/mp4",
		"--tag", "handoff",
		"--metadata", "source=cli",
		"--yes",
		"--retries", "1",
	}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/uploads":
			_, _ = fmt.Fprintf(w, `{"upload":{"method":"PUT","url":%q,"expires_at":"2026-06-02T12:15:00Z","expires_in_seconds":900,"blob_name":"fp-api/2026/06/02/abc-employee-a-clip.mp4","storage_bucket_id":7,"account_id":123,"album_id":77,"user_id":5,"filename":"employee-a-clip.mp4","byte_size":11,"content_type":"video/mp4","headers":{"x-ms-blob-type":"BlockBlob","Content-Type":"video/mp4"},"completion":{"method":"POST","path":"/api/v1/uploads/complete"}}}`, signedURL)
		case "/api/v1/uploads/complete":
			_, _ = io.WriteString(w, `{"content":{"id":42,"file_name":"employee-a-clip.mp4"},"upload":{"completed":true,"created":true,"account_id":123,"album_id":77,"storage_bucket_id":7,"blob_name":"fp-api/2026/06/02/abc-employee-a-clip.mp4"}}`)
		default:
			http.NotFound(w, r)
		}
	})
	if err != nil {
		t.Fatalf("media upload error: %v stdout=%s stderr=%s", err, stdout, stderr)
	}
	if strings.Contains(stdout, "SECRET") || strings.Contains(stderr, "SECRET") || strings.Contains(stdout, signedURL) {
		t.Fatalf("signed URL leaked stdout=%q stderr=%q", stdout, stderr)
	}
	if putHits != 2 {
		t.Fatalf("direct upload hits = %d, want retry then success", putHits)
	}
	if uploadedBody != "video-bytes" {
		t.Fatalf("uploaded body = %q", uploadedBody)
	}
	if uploadContentType != "video/mp4" {
		t.Fatalf("direct upload content-type = %q", uploadContentType)
	}
	if len(cap.requests) != 2 {
		t.Fatalf("API requests = %d, want preflight and completion", len(cap.requests))
	}
	preflight := requestBodyMap(t, cap.requests[0])
	if preflight["account_id"] != "123" || preflight["album_id"] != "77" || preflight["user_id"] != "5" {
		t.Fatalf("preflight body = %#v", preflight)
	}
	if preflight["filename"] != "employee-a-clip.mp4" || preflight["content_type"] != "video/mp4" {
		t.Fatalf("preflight file fields = %#v", preflight)
	}
	if tags := preflight["tags"].([]any); len(tags) != 1 || tags[0] != "handoff" {
		t.Fatalf("preflight tags = %#v", preflight["tags"])
	}
	if metadata := preflight["metadata"].(map[string]any); metadata["source"] != "cli" {
		t.Fatalf("preflight metadata = %#v", metadata)
	}
	completion := requestBodyMap(t, cap.requests[1])
	if completion["blob_name"] != "fp-api/2026/06/02/abc-employee-a-clip.mp4" || completion["storage_bucket_id"].(float64) != 7 {
		t.Fatalf("completion body = %#v", completion)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("upload stdout JSON: %v\n%s", err, stdout)
	}
	if summary := output["summary"].(map[string]any); summary["completed"].(float64) != 1 {
		t.Fatalf("upload summary = %#v", summary)
	}
}

func TestMediaUploadFailureRedactsSignedURL(t *testing.T) {
	dir := t.TempDir()
	file := writeTestFile(t, dir, "failed-clip.mp4", "video-bytes")

	uploadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "storage rejected https://storage.example/failed-clip.mp4?sig=SECRET", http.StatusForbidden)
	}))
	defer uploadSrv.Close()
	signedURL := uploadSrv.URL + "/failed-clip.mp4?sig=SECRET"

	stdout, stderr, _, err := runWithAPIServer(t, []string{
		"media", "upload", file,
		"--account-id", "123",
		"--content-type", "video/mp4",
		"--retries", "0",
	}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/uploads" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"upload":{"method":"PUT","url":%q,"blob_name":"fp-api/2026/06/02/failed-clip.mp4","storage_bucket_id":7,"filename":"failed-clip.mp4","byte_size":11,"content_type":"video/mp4","headers":{"x-ms-blob-type":"BlockBlob","Content-Type":"video/mp4"},"completion":{"method":"POST","path":"/api/v1/uploads/complete"}}}`, signedURL)
	})
	if err == nil {
		t.Fatalf("expected failed upload stdout=%s stderr=%s", stdout, stderr)
	}
	if strings.Contains(stdout, "SECRET") || strings.Contains(stderr, "SECRET") || strings.Contains(stdout, signedURL) || strings.Contains(stderr, signedURL) {
		t.Fatalf("signed URL leaked stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout, "[REDACTED]") {
		t.Fatalf("expected redacted URL marker in stdout=%q", stdout)
	}
}

func TestMediaShareURLRequiresConfirmationAndBuildsRequest(t *testing.T) {
	stdout, stderr, cap, err := runWithAPIServer(t, []string{"media", "share-url", "42", "--account-id", "123"}, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to %s", r.URL.String())
	})
	if err == nil {
		t.Fatalf("expected share confirmation error stdout=%s stderr=%s", stdout, stderr)
	}
	if cap.hits != 0 {
		t.Fatalf("share confirmation failure should not call API, got %d hits", cap.hits)
	}

	stdout, stderr, cap, err = runWithAPIServer(t, []string{"media", "share-url", "42", "--account-id", "123", "--yes"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/contents/42/share" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"share":{"id":42,"access":"public_link","public":true,"url":"https://footagepal.test/s/share-key","download_url":"https://footagepal.test/s/share-key/download","warning":"Anyone with this URL can view or download this content until the share-key scheme is changed."}}`)
	})
	if err != nil {
		t.Fatalf("share-url error: %v stdout=%s stderr=%s", err, stdout, stderr)
	}
	if values := mustParseQuery(t, cap.rawQuery); values.Get("account_id") != "123" {
		t.Fatalf("share query = %s", cap.rawQuery)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("share stdout JSON: %v\n%s", err, stdout)
	}
	if share := output["share"].(map[string]any); share["access"] != "public_link" || share["public"] != true {
		t.Fatalf("share payload = %#v", share)
	}
}

func TestAgentContextIncludesMediaAndExitCodes(t *testing.T) {
	stdout, _, err := runRoot(t, "agent-context")
	if err != nil {
		t.Fatalf("agent-context error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(stdout), &body); err != nil {
		t.Fatalf("agent-context JSON: %v", err)
	}
	commands := body["commands"].(map[string]any)
	if _, ok := commands["media"]; !ok {
		t.Fatalf("agent context missing media command")
	}
	if _, ok := commands["albums"]; !ok {
		t.Fatalf("agent context missing albums command")
	}
	media := commands["media"].(map[string]any)
	mediaSubs := media["subcommands"].(map[string]any)
	if _, ok := mediaSubs["upload"]; !ok {
		t.Fatalf("agent context missing media upload command")
	}
	if _, ok := mediaSubs["share-url"]; !ok {
		t.Fatalf("agent context missing media share-url command")
	}
	searchFlags := mediaSubs["search"].(map[string]any)["flags"].(map[string]any)
	if _, ok := searchFlags["--album-id"]; !ok {
		t.Fatalf("agent context missing media search --album-id")
	}
	exitCodes := body["exit_codes"].(map[string]any)
	if exitCodes["3"] == "" {
		t.Fatalf("agent context missing auth exit code")
	}
	enums := body["enums"].(map[string]any)
	if _, ok := enums["upload_status"]; !ok {
		t.Fatalf("agent context missing upload_status enum")
	}
}

func mustParseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse query %q: %v", raw, err)
	}
	return values
}

func lastRequest(t *testing.T, cap *captured) capturedRequest {
	t.Helper()
	if cap == nil || len(cap.requests) == 0 {
		t.Fatalf("no captured requests")
	}
	return cap.requests[len(cap.requests)-1]
}

func requestBodyMap(t *testing.T, req capturedRequest) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal([]byte(req.body), &body); err != nil {
		t.Fatalf("request body JSON: %v\n%s", err, req.body)
	}
	return body
}

func writeTestFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	return path
}
