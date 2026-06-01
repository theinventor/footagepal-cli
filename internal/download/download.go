package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/theinventor/footagepal-cli/internal/client"
)

type CollisionPolicy string

const (
	CollisionSkip      CollisionPolicy = "skip"
	CollisionRename    CollisionPolicy = "rename"
	CollisionOverwrite CollisionPolicy = "overwrite"
)

type Target struct {
	Path   string
	Status string
}

type FetchResult struct {
	Bytes int64
}

type FetchOptions struct {
	Retries int
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

func SafeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	name = unsafeName.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	return name
}

func SafeRelativeFolder(folder string) (string, error) {
	folder = strings.TrimSpace(strings.ReplaceAll(folder, "\\", "/"))
	if folder == "" || folder == "." {
		return "", nil
	}
	if strings.HasPrefix(folder, "/") {
		return "", fmt.Errorf("folder must be relative")
	}
	parts := strings.Split(folder, "/")
	safe := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", fmt.Errorf("folder must not contain path traversal")
		}
		safe = append(safe, SafeFilename(part))
	}
	if len(safe) == 0 {
		return "", nil
	}
	return filepath.Join(safe...), nil
}

func ResolveTarget(outputDir, id, filename, folder string, preserveFolders bool, policy CollisionPolicy) (Target, error) {
	if outputDir == "" {
		return Target{}, fmt.Errorf("output directory is required")
	}
	if err := ensureOutputDir(outputDir); err != nil {
		return Target{}, err
	}
	base := SafeFilename(filename)
	if id != "" {
		base = SafeFilename(id + "_" + base)
	}
	dir := outputDir
	if preserveFolders {
		rel, err := SafeRelativeFolder(folder)
		if err != nil {
			return Target{}, err
		}
		if rel != "" {
			dir = filepath.Join(outputDir, rel)
			if err := ensureOutputDir(dir); err != nil {
				return Target{}, err
			}
		}
	}
	original := filepath.Join(dir, base)
	path := original
	path, err := resolveCollision(path, policy)
	if err != nil {
		return Target{}, err
	}
	status := "planned"
	if policy == CollisionRename && path != original {
		status = "renamed"
	}
	if policy == CollisionOverwrite {
		if _, err := os.Lstat(path); err == nil {
			status = "overwritten"
		}
	}
	return Target{Path: path, Status: status}, nil
}

func ensureOutputDir(dir string) error {
	if info, err := os.Lstat(dir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output directory must not be a symlink: %s", dir)
		}
		if !info.IsDir() {
			return fmt.Errorf("output path is not a directory: %s", dir)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0700)
}

func resolveCollision(path string, policy CollisionPolicy) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to write through symlink target: %s", path)
	}
	switch policy {
	case CollisionSkip:
		return "", fmt.Errorf("target exists: %s", path)
	case CollisionOverwrite:
		return path, nil
	case CollisionRename:
		ext := filepath.Ext(path)
		stem := strings.TrimSuffix(path, ext)
		for i := 1; i <= 10000; i++ {
			candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
			if info, err := os.Lstat(candidate); err == nil {
				if info.Mode()&os.ModeSymlink != 0 {
					return "", fmt.Errorf("refusing to write through symlink target: %s", candidate)
				}
				continue
			} else if os.IsNotExist(err) {
				return candidate, nil
			} else {
				return "", err
			}
		}
		return "", fmt.Errorf("could not find free filename for %s", path)
	default:
		return "", fmt.Errorf("unknown collision policy %q", policy)
	}
}

func FetchURL(ctx context.Context, httpClient *http.Client, rawURL, targetPath string, opts FetchOptions) (FetchResult, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	if targetPath == "" {
		return FetchResult{}, fmt.Errorf("target path is required")
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0700); err != nil {
		return FetchResult{}, err
	}
	tmp := targetPath + ".part"
	_ = os.Remove(tmp)

	attempts := opts.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		n, err := fetchOnce(ctx, httpClient, rawURL, tmp)
		if err == nil {
			if err := os.Rename(tmp, targetPath); err != nil {
				_ = os.Remove(tmp)
				return FetchResult{}, err
			}
			return FetchResult{Bytes: n}, nil
		}
		_ = os.Remove(tmp)
		lastErr = err
		if attempt < attempts {
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
	}
	return FetchResult{}, lastErr
}

func fetchOnce(ctx context.Context, httpClient *http.Client, rawURL, tmp string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build signed download request %s: %w", client.RedactURL(rawURL), err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch signed download URL %s: %s", client.RedactURL(rawURL), redactErrorText(err.Error(), rawURL))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("fetch signed download URL %s: HTTP %d", client.RedactURL(rawURL), resp.StatusCode)
	}
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return n, err
	}
	if err := out.Close(); err != nil {
		return n, err
	}
	return n, nil
}

func redactErrorText(message, rawURL string) string {
	redacted := client.RedactURL(rawURL)
	message = strings.ReplaceAll(message, rawURL, redacted)
	if i := strings.Index(rawURL, "?"); i >= 0 {
		message = strings.ReplaceAll(message, rawURL[i+1:], "[REDACTED]")
	}
	return message
}
