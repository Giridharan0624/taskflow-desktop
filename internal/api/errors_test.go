package api

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// ════════════════════════════════════════════════════════════════════════════
// sanitizeErrorBody — H-API-1
// ════════════════════════════════════════════════════════════════════════════

func TestSanitizeErrorBody_plainText(t *testing.T) {
	got := sanitizeErrorBody([]byte(`{"error":"not found"}`))
	want := `{"error":"not found"}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeErrorBody_redactsJWT(t *testing.T) {
	// Any eyJ... . xxx . yyy triplet should be replaced with [REDACTED].
	body := []byte(`{"error":"bad token","token":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"}`)
	got := sanitizeErrorBody(body)
	if strings.Contains(got, "eyJ") {
		t.Fatalf("expected JWT to be redacted, got %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker, got %q", got)
	}
}

func TestSanitizeErrorBody_stripsControlChars(t *testing.T) {
	// Null bytes, escape sequences, etc. must be removed — newline and tab kept.
	body := []byte("before\x00\x01\x02\x1bafter\nline2\tcol2")
	got := sanitizeErrorBody(body)
	want := "beforeafter\nline2\tcol2"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeErrorBody_truncatesLongBodies(t *testing.T) {
	// A 10 KB error page must not blow up the returned error.
	// The cap is 200 bytes of payload + the 3-byte UTF-8 ellipsis (…).
	long := strings.Repeat("a", 10000)
	got := sanitizeErrorBody([]byte(long))
	const maxBytes = 200 + len("…") // "…" is 3 bytes in UTF-8
	if len(got) > maxBytes {
		t.Fatalf("expected byte length <= %d, got %d", maxBytes, len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncation marker, got suffix %q", got[len(got)-5:])
	}
}

func TestSanitizeErrorBody_emptyIsEmpty(t *testing.T) {
	if got := sanitizeErrorBody(nil); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := sanitizeErrorBody([]byte{}); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSanitizeErrorBody_trimsWhitespace(t *testing.T) {
	got := sanitizeErrorBody([]byte("   \n  payload  \n "))
	if got != "payload" {
		t.Fatalf("expected trimmed payload, got %q", got)
	}
}

// ════════════════════════════════════════════════════════════════════════════
// sentinelForStatus — H-API-2
// ════════════════════════════════════════════════════════════════════════════

func TestSentinelForStatus(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{200, nil},
		{201, nil},
		{301, nil},
		{400, ErrBadRequest},
		{401, ErrUnauthorized},
		{403, ErrForbidden},
		{404, ErrNotFound},
		{418, nil}, // unmapped 4xx falls through to generic
		{500, ErrServerError},
		{502, ErrServerError},
		{503, ErrServerError},
		{599, ErrServerError},
	}
	for _, tc := range cases {
		got := sentinelForStatus(tc.status)
		if got != tc.want {
			t.Errorf("status %d: got %v, want %v", tc.status, got, tc.want)
		}
	}
}

// ════════════════════════════════════════════════════════════════════════════
// End-to-end: errors.Is through the wrapped chain
// ════════════════════════════════════════════════════════════════════════════

func TestWrappedSentinel_errorsIs(t *testing.T) {
	// Simulate the shape apiError produces: fmt.Errorf("%s: %w (HTTP %d): %s", ...)
	// The point is to prove that errors.Is survives the wrap so callers can
	// actually drive UI off the sentinel (auth:expired etc).
	wrapped := fmt.Errorf("%s: %w (HTTP %d): %s",
		"sign-in", ErrUnauthorized, 401, "token expired")

	if !errors.Is(wrapped, ErrUnauthorized) {
		t.Fatalf("errors.Is failed for wrapped ErrUnauthorized: %v", wrapped)
	}
	if errors.Is(wrapped, ErrServerError) {
		t.Fatalf("wrapped ErrUnauthorized must not match ErrServerError")
	}
	if !strings.Contains(wrapped.Error(), "sign-in") {
		t.Fatalf("expected action in message, got %q", wrapped.Error())
	}
	if !strings.Contains(wrapped.Error(), "unauthorized") {
		t.Fatalf("expected sentinel word in message, got %q", wrapped.Error())
	}
}
