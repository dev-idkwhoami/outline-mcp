package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"outline-mcp/internal/fileutil"
)

type Config struct {
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key,omitempty"`
	History        bool   `json:"history"`
	HistoryLimit   int    `json:"history_limit"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func DefaultPath() (string, error) {
	if p := os.Getenv("OUTLINE_CONFIG"); p != "" {
		return filepath.Abs(p)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "outline-mcp", "config.json"), nil
}

func Load(path string) (Config, error) {
	c := Config{BaseURL: "https://app.getoutline.com", HistoryLimit: 500, TimeoutSeconds: 60}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if err == nil {
		if err = json.Unmarshal(data, &c); err != nil {
			return c, fmt.Errorf("invalid config %s: %w", path, err)
		}
	}
	for name, dst := range map[string]*string{"OUTLINE_BASE_URL": &c.BaseURL, "OUTLINE_API_KEY": &c.APIKey} {
		if v, ok := os.LookupEnv(name); ok {
			*dst = v
		}
	}
	if v, ok := os.LookupEnv("OUTLINE_HISTORY"); ok {
		c.History, err = strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("OUTLINE_HISTORY must be true or false")
		}
	}
	for name, dst := range map[string]*int{"OUTLINE_HISTORY_LIMIT": &c.HistoryLimit, "OUTLINE_TIMEOUT_SECONDS": &c.TimeoutSeconds} {
		if v, ok := os.LookupEnv(name); ok {
			*dst, err = strconv.Atoi(v)
			if err != nil {
				return c, fmt.Errorf("%s must be an integer", name)
			}
		}
	}
	return c, nil
}

func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("base URL must be an absolute HTTPS URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("base URL must not contain credentials, query parameters, or a fragment")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || ip != nil && ip.IsLoopback())) {
		return "", fmt.Errorf("use HTTPS (HTTP is allowed only for loopback development servers)")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/api") {
		u.Path += "/api"
	}
	u.RawPath = ""
	return u.String(), nil
}

func (c *Config) Validate() error {
	normalized, err := NormalizeURL(c.BaseURL)
	if err != nil {
		return err
	}
	c.BaseURL = normalized
	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.APIKey == "" {
		return fmt.Errorf("API key required: run outline-mcp setup or set OUTLINE_API_KEY")
	}
	if strings.ContainsAny(c.APIKey, "\r\n") {
		return fmt.Errorf("API key cannot contain newlines")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 {
		return fmt.Errorf("timeout_seconds must be between 1 and 600")
	}
	if c.HistoryLimit < 1 || c.HistoryLimit > 10000 {
		return fmt.Errorf("history_limit must be between 1 and 10000")
	}
	return nil
}

func Save(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.Write(path, append(b, '\n'), 0600)
}
