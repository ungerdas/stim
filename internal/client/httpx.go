package client

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
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

// TrackerDomain reduces a tracker announce URL, or a bare host name, to the
// site it belongs to: "https://tracker.example.org:443/a/announce" and
// "example.org" both become "example.org". Clients report trackers at
// different levels of detail (qBittorrent the full URL, Deluge a shortened
// host), and this makes them group together. IP addresses and single-label
// hosts are returned as they are.
func TrackerDomain(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	host := s
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return s
		}
		host = u.Hostname()
	} else if h, _, err := net.SplitHostPort(s); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return host
	}
	if domain, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return domain
	}
	return host
}
