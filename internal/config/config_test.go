package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigSaveLoadAndDefaultProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv(EnvConfig, path)

	f := &File{}
	f.Put("main", Profile{APIURL: "https://footagepal.com", APIToken: "fp_secret"})
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.DefaultProfile != "main" {
		t.Fatalf("default profile = %q, want main", loaded.DefaultProfile)
	}
	p, ok := loaded.Get("")
	if !ok || p.APIToken != "fp_secret" {
		t.Fatalf("default profile did not round-trip: %#v ok=%v", p, ok)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat config: %v", err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("config mode = %v, want 0600", got)
		}
	}
}

func TestConfigDeletePicksDeterministicDefault(t *testing.T) {
	f := &File{}
	f.Put("zeta", Profile{APIURL: "https://z.example"})
	f.Put("alpha", Profile{APIURL: "https://a.example"})

	if !f.Delete("zeta") {
		t.Fatalf("Delete returned false")
	}
	if f.DefaultProfile != "alpha" {
		t.Fatalf("default profile = %q, want alpha", f.DefaultProfile)
	}
}
