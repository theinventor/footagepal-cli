package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/theinventor/footagepal-cli/internal/config"
	"github.com/theinventor/footagepal-cli/internal/credstore"
)

const (
	EnvAPIToken = "FOOTAGEPAL_API_TOKEN"
	EnvAPIURL   = "FOOTAGEPAL_API_URL"

	DefaultAPIURL   = "https://footagepal.com"
	AuthScheme      = "token"
	UserAgentPrefix = "footagepal-cli"
)

type Client struct {
	BaseURL    string
	APIToken   string
	HTTPClient *http.Client
	Version    string
	Source     string
	Backend    string
}

func New() *Client {
	return NewWithProfile("")
}

func NewWithProfile(profile string) *Client {
	c := &Client{HTTPClient: &http.Client{Timeout: 30 * time.Second}}

	if profile != "" {
		if f, err := config.Load(); err == nil {
			if p, ok := f.Get(profile); ok {
				return c.loadFromProfile(profile, p).fillDefaults()
			}
		}
		if envURL := os.Getenv(EnvAPIURL); envURL != "" {
			c.BaseURL = strings.TrimRight(envURL, "/")
		}
		return c.fillDefaults()
	}

	if envToken := os.Getenv(EnvAPIToken); envToken != "" {
		c.BaseURL = strings.TrimRight(os.Getenv(EnvAPIURL), "/")
		c.APIToken = envToken
		c.Source = "env"
		c.Backend = credstore.BackendEnv
		return c.fillDefaults()
	}

	if f, err := config.Load(); err == nil {
		if p, ok := f.Get(""); ok {
			return c.loadFromProfile(f.DefaultProfile, p).fillDefaults()
		}
	}

	if envURL := os.Getenv(EnvAPIURL); envURL != "" {
		c.BaseURL = strings.TrimRight(envURL, "/")
	}
	return c.fillDefaults()
}

func (c *Client) fillDefaults() *Client {
	if c.BaseURL == "" {
		c.BaseURL = DefaultAPIURL
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return c
}

func (c *Client) loadFromProfile(name string, p *config.Profile) *Client {
	c.BaseURL = strings.TrimRight(p.APIURL, "/")
	c.Source = "profile:" + name
	if p.Backend == "" {
		c.Backend = credstore.BackendFile
	} else {
		c.Backend = p.Backend
	}
	if secret, err := credstore.Get(name, p.Backend, p.APIToken); err == nil {
		c.APIToken = secret
	}
	return c
}

func (c *Client) MaskedAPIToken() string {
	return MaskSecret(c.APIToken)
}

func MaskSecret(secret string) string {
	if secret == "" {
		return "(none)"
	}
	if len(secret) < 12 {
		return "***"
	}
	return secret[:8] + "..." + secret[len(secret)-4:]
}

func (c *Client) Do(method, path string, body any, query url.Values) (*http.Response, error) {
	u := c.BaseURL + path
	if query != nil && len(query) > 0 {
		u += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequest(method, u, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgentPrefix+"/"+c.Version)
	if c.APIToken != "" {
		req.Header.Set("Authorization", AuthScheme+" "+c.APIToken)
	}
	return c.HTTPClient.Do(req)
}

func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.RawQuery != "" {
		u.RawQuery = "[REDACTED]"
	}
	u.Fragment = ""
	return u.String()
}
