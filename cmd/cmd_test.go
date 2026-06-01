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
		cap.hits++
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.rawQuery = r.URL.RawQuery
		cap.authHeader = r.Header.Get("Authorization")
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
	exitCodes := body["exit_codes"].(map[string]any)
	if exitCodes["3"] == "" {
		t.Fatalf("agent context missing auth exit code")
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
