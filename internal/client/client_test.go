package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
)

func TestErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{fmt.Errorf("%w: wrong password", ErrAuth), "auth"},
		{fmt.Errorf("%w (HTTP 404)", ErrNotClient), "wrong-type"},
		{&url.Error{Op: "Get", URL: "http://x", Err: errors.New("connection refused")}, "unreachable"},
		{context.DeadlineExceeded, "unreachable"},
		{errors.New("something else"), "other"},
	}
	for _, c := range cases {
		if got := ErrorCode(c.err); got != c.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestBaseURL(t *testing.T) {
	ok := map[string]string{
		"http://10.0.0.2:8080/":            "http://10.0.0.2:8080",
		" https://box.example/user/deluge": "https://box.example/user/deluge",
	}
	for in, want := range ok {
		got, err := BaseURL(in)
		if err != nil || got != want {
			t.Errorf("BaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "10.0.0.2:8080", "ftp://x", "http://"} {
		if _, err := BaseURL(in); err == nil {
			t.Errorf("BaseURL(%q) should fail", in)
		}
	}
}

func TestTrackerDomain(t *testing.T) {
	cases := map[string]string{
		"": "",
		"https://tracker.example.org:443/a/announce": "example.org",
		"http://norbits.net/announce.php?passkey=x":  "norbits.net",
		"example.org":                                "example.org",
		"tracker.example.invalid":                    "example.invalid",
		"udp://tracker.opentrackr.org:1337/announce": "opentrackr.org",
		"http://tracker.example.co.uk/announce":      "example.co.uk",
		"http://10.0.0.2:6969/announce":              "10.0.0.2",
		"10.0.0.2":                                   "10.0.0.2",
		"http://[2001:db8::1]:6969/announce":         "2001:db8::1",
		"http://localhost:6969/announce":             "localhost",
		"Tracker.Example.ORG.":                       "example.org",
	}
	for in, want := range cases {
		if got := TrackerDomain(in); got != want {
			t.Errorf("TrackerDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
