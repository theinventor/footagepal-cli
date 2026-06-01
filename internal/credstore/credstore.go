package credstore

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/zalando/go-keyring"
)

const (
	BackendFile     = "file"
	BackendKeychain = "keychain"
	BackendEnv      = "env"
	BackendAuto     = "auto"

	Service            = "footagepal-cli"
	EnvDisableKeychain = "FOOTAGEPAL_DISABLE_KEYCHAIN"
	EnvStorage         = "FOOTAGEPAL_STORAGE"
)

var ErrNotFound = errors.New("credstore: no token stored for profile")

type keyringIface interface {
	Set(service, user, password string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

type realKeyring struct{}

func (realKeyring) Set(s, u, p string) error { return keyring.Set(s, u, p) }

func (realKeyring) Get(s, u string) (string, error) {
	v, err := keyring.Get(s, u)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func (realKeyring) Delete(s, u string) error {
	err := keyring.Delete(s, u)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

var active keyringIface = realKeyring{}

type MemKeyring struct {
	mu                        sync.Mutex
	data                      map[string]string
	FailGet, FailSet, FailDel bool
}

func NewMemKeyring() *MemKeyring { return &MemKeyring{data: map[string]string{}} }

func memKey(s, u string) string { return s + "::" + u }

func (m *MemKeyring) Set(service, user, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailSet {
		return errors.New("MemKeyring: simulated set failure")
	}
	m.data[memKey(service, user)] = password
	return nil
}

func (m *MemKeyring) Get(service, user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailGet {
		return "", errors.New("MemKeyring: simulated get failure")
	}
	v, ok := m.data[memKey(service, user)]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m *MemKeyring) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailDel {
		return errors.New("MemKeyring: simulated delete failure")
	}
	delete(m.data, memKey(service, user))
	return nil
}

func UseMockKeyring() (*MemKeyring, func()) {
	m := NewMemKeyring()
	prev := active
	active = m
	return m, func() { active = prev }
}

func KeychainAvailable() bool {
	if os.Getenv(EnvDisableKeychain) != "" {
		return false
	}
	_, err := active.Get(Service, "__footagepal_keychain_probe__")
	return err == nil || errors.Is(err, ErrNotFound)
}

func ResolveBackend(storage string) (string, error) {
	if storage == "" {
		storage = os.Getenv(EnvStorage)
	}
	if storage == "" {
		storage = BackendAuto
	}
	switch storage {
	case BackendKeychain:
		if !KeychainAvailable() {
			return "", fmt.Errorf("keychain backend requested but no OS keyring is available on %s (set FOOTAGEPAL_STORAGE=file or unset FOOTAGEPAL_DISABLE_KEYCHAIN)", runtime.GOOS)
		}
		return BackendKeychain, nil
	case BackendFile:
		return BackendFile, nil
	case BackendAuto:
		if KeychainAvailable() {
			return BackendKeychain, nil
		}
		return BackendFile, nil
	default:
		return "", fmt.Errorf("unknown storage backend %q (want keychain|file|auto)", storage)
	}
}

func Put(profile, backend, secret string) (string, error) {
	switch backend {
	case BackendKeychain:
		if err := active.Set(Service, profile, secret); err != nil {
			return "", fmt.Errorf("keychain set: %w", err)
		}
		return BackendKeychain, nil
	case BackendFile, "":
		return BackendFile, nil
	default:
		return "", fmt.Errorf("credstore.Put: unsupported backend %q", backend)
	}
}

func Get(profile, backend, fileSecret string) (string, error) {
	switch backend {
	case BackendKeychain:
		v, err := active.Get(Service, profile)
		if err != nil {
			return "", err
		}
		return v, nil
	case BackendFile, "":
		return fileSecret, nil
	default:
		return "", fmt.Errorf("credstore.Get: unsupported backend %q", backend)
	}
}

func Delete(profile string) error {
	if err := active.Delete(Service, profile); err != nil {
		return fmt.Errorf("keychain delete: %w", err)
	}
	return nil
}

func Describe(backend string) string {
	switch backend {
	case BackendKeychain:
		return "keychain"
	case BackendFile:
		return "file"
	case BackendEnv:
		return "env"
	case "":
		return "file (legacy)"
	default:
		return backend
	}
}
