package client

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnvResolutionAndAuthHeader(t *testing.T) {
	var auth, ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		ua = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	t.Setenv(EnvAPIURL, srv.URL)
	t.Setenv(EnvAPIToken, "fp_test_secret_123456789")

	c := New()
	c.Version = "vtest"
	resp, err := c.Do(http.MethodGet, "/api/v1/me", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	if auth != "token fp_test_secret_123456789" {
		t.Fatalf("Authorization = %q", auth)
	}
	if ua != "footagepal-cli/vtest" {
		t.Fatalf("User-Agent = %q", ua)
	}
	if c.Source != "env" {
		t.Fatalf("Source = %q, want env", c.Source)
	}
}

func TestMaskSecretDoesNotExposeRawShortOrLongSecrets(t *testing.T) {
	if got := MaskSecret("short"); got != "***" {
		t.Fatalf("short mask = %q", got)
	}
	got := MaskSecret("fp_abcdefghijklmnopqrstuvwxyz")
	if got == "fp_abcdefghijklmnopqrstuvwxyz" || got == "" {
		t.Fatalf("long mask exposed raw token: %q", got)
	}
}

func TestRedactURLRemovesQueryAndFragment(t *testing.T) {
	got := RedactURL("https://example.com/file.mp4?sig=secret#frag")
	if got != "https://example.com/file.mp4?[REDACTED]" {
		t.Fatalf("redacted URL = %q", got)
	}
}
