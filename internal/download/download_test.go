package download

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSafeFilenameAndFolderRejectTraversal(t *testing.T) {
	if got := SafeFilename("../../DJI 0001?.MP4"); got != "DJI 0001_.MP4" {
		t.Fatalf("SafeFilename = %q", got)
	}
	if _, err := SafeRelativeFolder("../private"); err == nil {
		t.Fatalf("SafeRelativeFolder should reject traversal")
	}
	if got, err := SafeRelativeFolder("2024/03/raw footage"); err != nil || got != filepath.Join("2024", "03", "raw footage") {
		t.Fatalf("SafeRelativeFolder = %q err=%v", got, err)
	}
}

func TestResolveTargetCollisionPolicies(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "42_DJI.MP4")
	if err := os.WriteFile(existing, []byte("old"), 0600); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	if _, err := ResolveTarget(dir, "42", "DJI.MP4", "", false, CollisionSkip); err == nil {
		t.Fatalf("skip policy should refuse existing target")
	}

	target, err := ResolveTarget(dir, "42", "DJI.MP4", "", false, CollisionRename)
	if err != nil {
		t.Fatalf("rename target: %v", err)
	}
	if target.Status != "renamed" || !strings.Contains(target.Path, " (1).MP4") {
		t.Fatalf("rename target = %#v", target)
	}

	target, err = ResolveTarget(dir, "42", "DJI.MP4", "", false, CollisionOverwrite)
	if err != nil {
		t.Fatalf("overwrite target: %v", err)
	}
	if target.Status != "overwritten" || target.Path != existing {
		t.Fatalf("overwrite target = %#v", target)
	}
}

func TestResolveTargetRefusesSymlinkTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows CI")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "42_DJI.MP4")
	if err := os.WriteFile(real, []byte("old"), 0600); err != nil {
		t.Fatalf("write real: %v", err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ResolveTarget(dir, "42", "DJI.MP4", "", false, CollisionOverwrite); err == nil {
		t.Fatalf("expected symlink target refusal")
	}
}

func TestFetchURLRedactsSignedURLOnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := FetchURL(ctx, &http.Client{Timeout: time.Second}, "http://127.0.0.1:1/file.mp4?sig=SECRET", filepath.Join(t.TempDir(), "file.mp4"), FetchOptions{})
	if err == nil {
		t.Fatalf("expected fetch error")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error exposed signed URL secret: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error did not show redaction marker: %v", err)
	}
}
