package client

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// NewHTTPClient returns an http.Client with a cookie jar (both qBittorrent and
// Deluge use session cookies) and the TLS settings from cfg.
func NewHTTPClient(cfg Config) *http.Client {
	jar, _ := cookiejar.New(nil)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.TLSSkipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in per instance
	}
	return &http.Client{Jar: jar, Transport: tr, Timeout: 30 * time.Second}
}

// BaseURL validates cfg.URL and returns it without a trailing slash.
func BaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("URL must start with http:// or https://")
	}
	if u.Host == "" {
		return "", fmt.Errorf("URL is missing a host")
	}
	return strings.TrimRight(raw, "/"), nil
}

// TrackerHost reduces a tracker announce URL to its host name.
func TrackerHost(announce string) string {
	if announce == "" {
		return ""
	}
	u, err := url.Parse(announce)
	if err != nil || u.Hostname() == "" {
		return announce
	}
	return u.Hostname()
}
