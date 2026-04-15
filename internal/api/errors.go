package api

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-resty/resty/v2"
)

// Sentinel errors. Callers use errors.Is(err, api.ErrUnauthorized) to drive
// re-auth UI, retry policy, etc. Wails serializes err.Error() across IPC so
// these strings are also user-facing — keep them short and neutral.
//
// See H-API-2 in docs/BUG-REPORT-GO.md.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrBadRequest   = errors.New("bad request")
	ErrServerError  = errors.New("server error")
)

// jwtLike matches anything that looks like a JWT (three base64url segments
// separated by dots). Used to redact tokens that may appear in reflected
// error bodies from the backend.
var jwtLike = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`)

// sanitizeErrorBody trims an HTTP response body for safe inclusion in an
// error message:
//   - Redacts JWT-like strings (defense in depth; the backend should never
//     reflect tokens in errors, but a misconfigured WAF or reverse proxy
//     sometimes echoes the Authorization header).
//   - Strips ASCII control characters except newline and tab.
//   - Caps the total length at 200 bytes so a 10 MB HTML error page can't
//     explode the returned error.
//
// See H-API-1 in docs/BUG-REPORT-GO.md.
func sanitizeErrorBody(b []byte) string {
	s := string(b)
	s = jwtLike.ReplaceAllString(s, "[REDACTED]")
	s = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// sentinelForStatus maps an HTTP status code to the matching sentinel error,
// or nil for 2xx / 3xx. Callers wrap the return value with fmt.Errorf using
// %w so errors.Is keeps working after the wrap.
func sentinelForStatus(status int) error {
	switch {
	case status == 401:
		return ErrUnauthorized
	case status == 403:
		return ErrForbidden
	case status == 404:
		return ErrNotFound
	case status == 400:
		return ErrBadRequest
	case status >= 500:
		return ErrServerError
	}
	return nil
}

// apiError is the single code path every client.go method uses to produce an
// error from a non-2xx resty response. It:
//   - Picks a sentinel (errors.Is works at the call site)
//   - Sanitizes the reflected body (H-API-1)
//   - Includes a short action verb so the user sees "sign-in: unauthorized"
//     instead of an opaque status code.
func apiError(action string, resp *resty.Response) error {
	status := resp.StatusCode()
	msg := sanitizeErrorBody(resp.Body())
	sentinel := sentinelForStatus(status)

	if sentinel != nil {
		if msg == "" {
			return fmt.Errorf("%s: %w (HTTP %d)", action, sentinel, status)
		}
		return fmt.Errorf("%s: %w (HTTP %d): %s", action, sentinel, status, msg)
	}
	if msg == "" {
		return fmt.Errorf("%s: HTTP %d", action, status)
	}
	return fmt.Errorf("%s: HTTP %d: %s", action, status, msg)
}
