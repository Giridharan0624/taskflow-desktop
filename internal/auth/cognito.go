package auth

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognito "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"

	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/state"
)

// cognitoCallTimeout bounds each AWS Cognito API call. Callers create a
// fresh context via context.WithTimeout(context.Background(),
// cognitoCallTimeout) and MUST `defer cancel()` immediately — a stale
// cancel function is a goroutine leak (see C-AUTH-3).
const cognitoCallTimeout = 15 * time.Second

const (
	// Keychain service name for storing tokens
	KeyringService = "taskflow-desktop"
)

// LoginResult is returned from the Login method.
// The Cognito challenge Session is intentionally NOT part of this struct:
// it must never cross the Wails IPC boundary into the JS renderer, where
// any script (including a compromised third-party dependency) could read it.
// The session is stored on Service and consumed by CompleteNewPasswordChallenge.
type LoginResult struct {
	Success             bool   `json:"success"`
	RequiresNewPassword bool   `json:"requiresNewPassword"`
	UserID              string `json:"userId,omitempty"`
	Email               string `json:"email,omitempty"`
	Name                string `json:"name,omitempty"`
}

// Tokens holds the Cognito JWT tokens.
type Tokens struct {
	IDToken      string `json:"idToken"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"` // Unix timestamp
}

// Service handles authentication with AWS Cognito.
//
// All fields below mu are guarded by mu. A single coarse mutex is
// deliberate: the auth service is low-frequency (one login, occasional
// refresh, one logout) and the Cognito InitiateAuth call dominates
// latency anyway. Holding the mutex across a 15-second Cognito call is
// fine because every auth operation is naturally serialized per user.
//
// See H-AUTH-4, H-AUTH-5 in docs/BUG-REPORT-GO.md.
type Service struct {
	client    *cognito.Client
	state     *state.AppState
	clientID  string // Cognito app client ID from config
	expectedIss string // https://cognito-idp.<region>.amazonaws.com/<poolID>

	mu     sync.Mutex
	tokens *Tokens

	// Challenge state for NEW_PASSWORD_REQUIRED. Never serialized over IPC —
	// kept entirely on the Go side so the JS renderer cannot read the
	// Cognito session token.
	lastLoginEmail   string
	lastLoginSession string
}

// NewService creates a new auth service.
func NewService(appState *state.AppState) *Service {
	cfg := config.Get()

	// Use anonymous credentials — Cognito InitiateAuth is a public (unauthenticated) API.
	client := cognito.New(cognito.Options{
		Region:      cfg.CognitoRegion,
		Credentials: aws.AnonymousCredentials{},
	})

	return &Service{
		client:    client,
		state:     appState,
		clientID:  cfg.CognitoClientID,
		// Cognito issues tokens with iss = https://cognito-idp.<region>.amazonaws.com/<poolID>.
		// We use this to reject tokens from the wrong pool during
		// client-side sanity checks (see verifyIDTokenClaims / M-AUTH-3).
		expectedIss: fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cfg.CognitoRegion, cfg.CognitoPoolID),
	}
}

// Login authenticates with Cognito using USER_PASSWORD_AUTH flow.
// Supports both email and Employee ID (e.g., NS-26FA95) — Employee IDs
// are resolved to email first via the /resolve-employee endpoint.
//
// Holds s.mu from the moment we look up the challenge state until the
// new tokens are committed. This prevents a concurrent GetIDToken from
// observing a half-committed state.
func (s *Service) Login(identifier, password string) (*LoginResult, error) {
	// Trim whitespace up front. The frontend trims too, but a modified
	// client (or a workflow that pipes an ID from the clipboard) can
	// ship `" user@x.com "` across IPC — without this Cognito rejects
	// with an opaque "UserNotFoundException" that looks like a wrong
	// password. Empty-string check catches DevTools-bypassed "required"
	// attributes.
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("email or employee ID is required")
	}
	email := identifier

	// If it looks like an Employee ID (not an email), resolve it. The
	// resolve call is a plain HTTP GET against an unauthenticated
	// endpoint — no state access — so it's safe to do outside the lock.
	if !strings.Contains(identifier, "@") && isEmployeeID(identifier) {
		resolved, err := s.resolveEmployeeID(identifier)
		if err != nil {
			return nil, fmt.Errorf("Employee ID not found: %w", err)
		}
		email = resolved
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Store email for NEW_PASSWORD_REQUIRED challenge
	s.lastLoginEmail = email

	input := &cognito.InitiateAuthInput{
		AuthFlow: types.AuthFlowTypeUserPasswordAuth,
		ClientId: aws.String(s.clientID),
		AuthParameters: map[string]string{
			"USERNAME": email,
			"PASSWORD": password,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), cognitoCallTimeout)
	defer cancel()
	result, err := s.client.InitiateAuth(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Handle NEW_PASSWORD_REQUIRED challenge (first-time login).
	// The Cognito session is stored internally and consumed by
	// CompleteNewPasswordChallenge — it never crosses the IPC boundary.
	if result.ChallengeName == types.ChallengeNameTypeNewPasswordRequired {
		if result.Session != nil {
			s.lastLoginSession = *result.Session
		}
		return &LoginResult{
			Success:             false,
			RequiresNewPassword: true,
		}, nil
	}

	// A successful login invalidates any pending challenge state.
	s.lastLoginSession = ""

	// Success — store tokens
	if result.AuthenticationResult == nil {
		return nil, fmt.Errorf("unexpected empty authentication result")
	}

	s.tokens = &Tokens{
		IDToken:      *result.AuthenticationResult.IdToken,
		AccessToken:  *result.AuthenticationResult.AccessToken,
		RefreshToken: *result.AuthenticationResult.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.AuthenticationResult.ExpiresIn) * time.Second).Unix(),
	}

	// Persist tokens to OS keychain
	if err := s.saveTokensToKeyring(); err != nil {
		return nil, fmt.Errorf("failed to save tokens: %w", err)
	}

	userInfo := s.decodeIDToken(s.tokens.IDToken)

	return &LoginResult{
		Success: true,
		UserID:  userInfo["sub"],
		Email:   userInfo["email"],
		Name:    userInfo["name"],
	}, nil
}

// CompleteNewPasswordChallenge handles the first-login password change.
// The Cognito challenge session is read from internal state (set during Login),
// never passed across the IPC boundary.
func (s *Service) CompleteNewPasswordChallenge(newPassword string) error {
	// Validate the password locally before wrapping the whole call in
	// the Service mutex + shipping it to Cognito. The frontend has
	// minLength={8} but that's HTML5, bypassable via DevTools; and an
	// empty string here produces an opaque AWS SDK error that looks
	// like a network failure to the user. Mirror the Cognito default
	// pool policy: ≥8 chars after trim. The Cognito call is still the
	// source of truth for upper/lower/digit rules.
	if len(strings.TrimSpace(newPassword)) < 8 {
		return fmt.Errorf("new password must be at least 8 characters")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastLoginSession == "" {
		return fmt.Errorf("no pending password challenge; please log in again")
	}

	input := &cognito.RespondToAuthChallengeInput{
		ChallengeName: types.ChallengeNameTypeNewPasswordRequired,
		ClientId:      aws.String(s.clientID),
		Session:       aws.String(s.lastLoginSession),
		ChallengeResponses: map[string]string{
			"NEW_PASSWORD": newPassword,
			"USERNAME":     s.getStoredUsernameLocked(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), cognitoCallTimeout)
	defer cancel()
	result, err := s.client.RespondToAuthChallenge(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to set new password: %w", err)
	}

	if result.AuthenticationResult == nil {
		return fmt.Errorf("unexpected empty authentication result after password change")
	}

	s.tokens = &Tokens{
		IDToken:      *result.AuthenticationResult.IdToken,
		AccessToken:  *result.AuthenticationResult.AccessToken,
		RefreshToken: *result.AuthenticationResult.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(result.AuthenticationResult.ExpiresIn) * time.Second).Unix(),
	}

	// Challenge consumed — clear it so a second call cannot replay.
	s.lastLoginSession = ""

	return s.saveTokensToKeyring()
}

// TryRestoreSession attempts to restore a session from stored tokens.
// If the stored ID token is still valid, it is committed immediately.
// If it has expired, we refresh OUT OF BAND — s.tokens stays nil until
// the refresh call succeeds, so a concurrent GetIDToken cannot observe
// stale tokens mid-refresh (H-AUTH-4).
func (s *Service) TryRestoreSession() error {
	tokens, err := s.loadTokensFromKeyring()
	if err != nil {
		return err
	}

	// Still valid — sanity-check claims before committing. The server
	// will verify the signature on every API call, but rejecting an
	// obviously-stale or wrong-pool token here avoids wiring the app
	// into a dead session. See M-AUTH-3.
	if time.Now().Unix() <= tokens.ExpiresAt-300 {
		if err := s.verifyIDTokenClaims(tokens.IDToken); err != nil {
			log.Printf("auth: stored id_token rejected (%v) — forcing re-login", err)
			return fmt.Errorf("stored session rejected: %w", err)
		}
		s.mu.Lock()
		s.tokens = tokens
		s.mu.Unlock()
		return nil
	}

	// Expired — refresh using the stored refresh token WITHOUT committing
	// the stale set to s.tokens first. Only on success do we expose the
	// new tokens to the rest of the app.
	refreshed, err := s.doRefreshCall(tokens.RefreshToken)
	if err != nil {
		return fmt.Errorf("stored session expired: %w", err)
	}
	// Cognito's refresh response does not include a new RefreshToken, so
	// reuse the one we loaded from keyring.
	refreshed.RefreshToken = tokens.RefreshToken
	// Same claim check on the refreshed token — a compromised token-
	// issuing endpoint is the only realistic way an iss/exp mismatch
	// shows up here, and the cost of the check is a few microseconds.
	if err := s.verifyIDTokenClaims(refreshed.IDToken); err != nil {
		return fmt.Errorf("refreshed token rejected: %w", err)
	}

	s.mu.Lock()
	s.tokens = refreshed
	s.mu.Unlock()

	return s.saveTokensToKeyring()
}

// GetIDToken returns a valid ID token, refreshing if needed. Takes the
// mutex exclusively so a mid-refresh mutation of s.tokens is atomic with
// respect to this read.
func (s *Service) GetIDToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tokens == nil {
		return "", fmt.Errorf("not authenticated")
	}

	// Refresh if token expires within 5 minutes. refreshTokensLocked
	// assumes s.mu is already held.
	if time.Now().Unix() > s.tokens.ExpiresAt-300 {
		if err := s.refreshTokensLocked(); err != nil {
			return "", fmt.Errorf("token refresh failed: %w", err)
		}
		if s.tokens == nil {
			return "", fmt.Errorf("token gone after refresh")
		}
	}

	return s.tokens.IDToken, nil
}

// Logout clears all stored tokens and any pending challenge state.
// Returns the first error encountered while deleting tokens from the OS
// keyring so callers can surface a genuinely broken logout instead of
// silently pretending it succeeded (H-AUTH-3).
func (s *Service) Logout() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = nil
	s.lastLoginSession = ""
	if err := s.deleteTokensFromKeyring(); err != nil {
		return fmt.Errorf("delete stored tokens: %w", err)
	}
	return nil
}

// doRefreshCall is the stateless Cognito refresh API call — it takes a
// refresh token and returns the new (Id/Access) tokens without touching
// s.tokens. Used by TryRestoreSession (to verify before committing) and
// by refreshTokensLocked (for in-session refresh).
func (s *Service) doRefreshCall(refreshToken string) (*Tokens, error) {
	if refreshToken == "" {
		return nil, errors.New("no refresh token available")
	}
	input := &cognito.InitiateAuthInput{
		AuthFlow: types.AuthFlowTypeRefreshTokenAuth,
		ClientId: aws.String(s.clientID),
		AuthParameters: map[string]string{
			"REFRESH_TOKEN": refreshToken,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), cognitoCallTimeout)
	defer cancel()
	result, err := s.client.InitiateAuth(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("cognito refresh: %w", err)
	}
	if result.AuthenticationResult == nil {
		return nil, fmt.Errorf("empty authentication result from refresh")
	}
	return &Tokens{
		IDToken:     *result.AuthenticationResult.IdToken,
		AccessToken: *result.AuthenticationResult.AccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(result.AuthenticationResult.ExpiresIn) * time.Second).Unix(),
		// RefreshToken intentionally empty — callers fill it in.
	}, nil
}

// refreshTokensLocked refreshes the in-session tokens. Caller MUST hold
// s.mu. On failure, clears s.tokens inline rather than calling Logout
// (which would try to re-acquire the mutex and deadlock).
func (s *Service) refreshTokensLocked() error {
	if s.tokens == nil || s.tokens.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	newTokens, err := s.doRefreshCall(s.tokens.RefreshToken)
	if err != nil {
		// Refresh token expired (>30 days) — user must re-login.
		// Clear inline; cannot call s.Logout() because we hold s.mu.
		s.tokens = nil
		s.lastLoginSession = ""
		if delErr := s.deleteTokensFromKeyring(); delErr != nil {
			log.Printf("keystore cleanup after refresh failure returned: %v", delErr)
		}
		return fmt.Errorf("refresh token expired, please login again: %w", err)
	}

	// Keep the existing refresh token — Cognito doesn't return one on refresh.
	s.tokens.IDToken = newTokens.IDToken
	s.tokens.AccessToken = newTokens.AccessToken
	s.tokens.ExpiresAt = newTokens.ExpiresAt

	return s.saveTokensToKeyring()
}

// verifyIDTokenClaims does CHEAP client-side sanity checks on a JWT
// before the Service stores it as the live session:
//
//   - exp is in the future (with 5-minute tolerance for clock skew)
//   - iss matches the configured Cognito pool
//
// This is NOT a signature check — the server still does that on every
// API call, which is the real security guarantee. Its job is to catch
// locally-obvious problems (stored token expired while the app was
// asleep; config pointed at the wrong Cognito pool; a corrupted
// keyring entry decoded to something token-shaped but meaningless) so
// the app surfaces a clean "please re-login" state instead of
// silently propagating a dead token into every API call. See M-AUTH-3.
func (s *Service) verifyIDTokenClaims(idToken string) error {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return errors.New("not a JWT (expected 3 segments)")
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("payload base64: %w", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(decoded, &raw); err != nil {
		return fmt.Errorf("payload json: %w", err)
	}
	// exp is a JSON number (float64 after unmarshal) per RFC 7519.
	expRaw, ok := raw["exp"].(float64)
	if !ok {
		return errors.New("missing or non-numeric exp claim")
	}
	// Allow 5 minutes of clock skew before rejecting — Cognito
	// tokens are valid for ~1 hour, so this is generous without
	// becoming a security hole.
	const skew = 5 * 60
	if int64(expRaw) < time.Now().Unix()-skew {
		return fmt.Errorf("token expired (exp=%d, now=%d)", int64(expRaw), time.Now().Unix())
	}
	// iss is a JSON string. If the caller built the Service against
	// the wrong Cognito pool/region, iss will mismatch — better to
	// fail now than to chase a "user has no tasks" ticket later
	// (which is exactly what "tokens from wrong pool" looks like).
	iss, _ := raw["iss"].(string)
	if iss != s.expectedIss {
		return fmt.Errorf("iss mismatch: token=%q expected=%q", iss, s.expectedIss)
	}
	return nil
}

// decodeIDToken extracts claims from the JWT without verification
// (verification happens server-side on every API call).
func (s *Service) decodeIDToken(idToken string) map[string]string {
	claims := make(map[string]string)
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return claims
	}

	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return claims
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(decoded, &raw); err != nil {
		return claims
	}

	for k, v := range raw {
		if str, ok := v.(string); ok {
			claims[k] = str
		}
	}

	return claims
}

// getStoredUsernameLocked returns the email for challenge responses.
// Caller must hold s.mu.
func (s *Service) getStoredUsernameLocked() string {
	if s.lastLoginEmail != "" {
		return s.lastLoginEmail
	}
	if s.tokens != nil {
		claims := s.decodeIDToken(s.tokens.IDToken)
		if email, ok := claims["email"]; ok {
			return email
		}
	}
	return ""
}

// employeeIDRegex matches Employee ID formats: NS-OWNER, NS-26AK76, NS-DEV-26AK76, EMP-0001
var employeeIDRegex = regexp.MustCompile(`(?i)^(EMP-\d+|[A-Z]{2,4}-[A-Z]{3}-\d{2}[A-Z0-9]+|[A-Z]{2,4}-[A-Z0-9]+)$`)

// isEmployeeID returns true if the string looks like an Employee ID (not an email).
func isEmployeeID(s string) bool {
	return employeeIDRegex.MatchString(s)
}

// unauthClient is a hardened HTTP client used for pre-auth calls such as
// resolveEmployeeID. It enforces TLS 1.3, a 10-second timeout, and
// rejects any redirect that drops off HTTPS. See H-AUTH-6.
var unauthClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-https redirect to %q", req.URL.String())
		}
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// resolveEmployeeID calls GET /resolve-employee?employeeId=... to get
// the email. This endpoint does not require authentication but we still
// hit it through the hardened unauthClient above:
//   - TLS 1.3 minimum (matches the main APIClient posture)
//   - Context-scoped timeout instead of the global http.DefaultClient
//     (which has no timeout at all)
//   - employeeID url.QueryEscape'd before interpolation so a crafted
//     input cannot inject a second query parameter
//
// See H-AUTH-6 and M-AUTH-1.
func (s *Service) resolveEmployeeID(employeeID string) (string, error) {
	cfg := config.Get()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reqURL := fmt.Sprintf(
		"%s/resolve-employee?employeeId=%s",
		strings.TrimRight(cfg.APIURL, "/"),
		url.QueryEscape(employeeID),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("build resolve request: %w", err)
	}

	resp, err := unauthClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("employee ID not found (status %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Email == "" {
		return "", fmt.Errorf("no email found for employee ID %s", employeeID)
	}

	return result.Email, nil
}
