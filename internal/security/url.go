// Package security holds small, self-contained helpers that exist to
// centralize trust-boundary decisions. Every place in the codebase that
// takes a server-controlled string and turns it into a network target
// or shell argument should go through these helpers.
//
// See the audit's Systemic Pattern P-4 (no validation layer between
// server strings and network operations).
package security

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ValidateHTTPSURL parses raw, rejects any scheme other than https, and
// rejects any host that is not in the allow list. Subdomains of an
// allowed host are accepted (so "github.com" in the list matches
// "api.github.com"). Callers must use the returned *url.URL — never
// re-inject the raw input into an http.Request.
//
// Used by:
//   - internal/updater: validates the binary download URL and the
//     SHA256SUMS URL from GitHub releases (C-UPD-2).
//   - internal/api: validates the S3 presigned upload URL returned
//     by our backend before we PUT a screenshot to it (C-API-1).
func ValidateHTTPSURL(raw string, allowedHosts []string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("URL must be https, got %q", u.Scheme)
	}
	// Reject URLs that carry userinfo (`https://user:pass@host/…` or
	// `https://token@host/…`). Without this guard a compromised or
	// malicious response from our backend could smuggle attacker-
	// controlled credentials into an HTTPS request to an allow-listed
	// host — the Go http.Client forwards `u.User` as HTTP Basic
	// authentication, and GitHub / AWS would then receive those
	// credentials as if we had authenticated. See V2-C1.
	if u.User != nil {
		return nil, errors.New("URL must not contain userinfo")
	}
	host := strings.ToLower(u.Hostname())
	for _, a := range allowedHosts {
		if host == a || strings.HasSuffix(host, "."+a) {
			return u, nil
		}
	}
	return nil, fmt.Errorf("host %q not in allowlist", host)
}
