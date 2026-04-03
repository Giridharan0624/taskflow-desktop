package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cognito "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"

	"taskflow-desktop/internal/config"
	"taskflow-desktop/internal/state"
)

const (
	// Keychain service name for storing tokens
	KeyringService = "taskflow-desktop"
)

// LoginResult is returned from the Login method.
type LoginResult struct {
	Success             bool   `json:"success"`
	RequiresNewPassword bool   `json:"requiresNewPassword"`
	Session             string `json:"session,omitempty"` // Used for NEW_PASSWORD_REQUIRED challenge
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
type Service struct {
	client         *cognito.Client
	tokens         *Tokens
	state          *state.AppState
	clientID       string // Cognito app client ID from config
	lastLoginEmail string // stored for NEW_PASSWORD_REQUIRED challenge
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
		client:   client,
		state:    appState,
		clientID: cfg.CognitoClientID,
	}
}

// Login authenticates with Cognito using USER_PASSWORD_AUTH flow.
// Supports both email and Employee ID (e.g., NS-26FA95) — Employee IDs
// are resolved to email first via the /resolve-employee endpoint.
func (s *Service) Login(identifier, password string) (*LoginResult, error) {
	email := identifier

	// If it looks like an Employee ID (not an email), resolve it
	if !strings.Contains(identifier, "@") && isEmployeeID(identifier) {
		resolved, err := s.resolveEmployeeID(identifier)
		if err != nil {
			return nil, fmt.Errorf("Employee ID not found: %w", err)
		}
		email = resolved
	}

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

	result, err := s.client.InitiateAuth(context.Background(), input)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Handle NEW_PASSWORD_REQUIRED challenge (first-time login)
	if result.ChallengeName == types.ChallengeNameTypeNewPasswordRequired {
		return &LoginResult{
			Success:             false,
			RequiresNewPassword: true,
			Session:             *result.Session,
		}, nil
	}

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
func (s *Service) CompleteNewPasswordChallenge(session, newPassword string) error {
	input := &cognito.RespondToAuthChallengeInput{
		ChallengeName: types.ChallengeNameTypeNewPasswordRequired,
		ClientId:      aws.String(s.clientID),
		Session:       aws.String(session),
		ChallengeResponses: map[string]string{
			"NEW_PASSWORD": newPassword,
			"USERNAME":     s.getStoredUsername(),
		},
	}

	result, err := s.client.RespondToAuthChallenge(context.Background(), input)
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

	return s.saveTokensToKeyring()
}

// TryRestoreSession attempts to restore a session from stored tokens.
func (s *Service) TryRestoreSession() error {
	tokens, err := s.loadTokensFromKeyring()
	if err != nil {
		return err
	}

	s.tokens = tokens

	// Check if the ID token is expired or about to expire
	if time.Now().Unix() > s.tokens.ExpiresAt-300 { // 5 min buffer
		// Try refreshing with the refresh token
		return s.refreshTokens()
	}

	return nil
}

// GetIDToken returns a valid ID token, refreshing if needed.
func (s *Service) GetIDToken() (string, error) {
	if s.tokens == nil {
		return "", fmt.Errorf("not authenticated")
	}

	// Refresh if token expires within 5 minutes
	if time.Now().Unix() > s.tokens.ExpiresAt-300 {
		if err := s.refreshTokens(); err != nil {
			return "", fmt.Errorf("token refresh failed: %w", err)
		}
	}

	return s.tokens.IDToken, nil
}

// Logout clears all stored tokens.
func (s *Service) Logout() {
	s.tokens = nil
	s.deleteTokensFromKeyring()
}

// refreshTokens uses the refresh token to get new ID and Access tokens.
func (s *Service) refreshTokens() error {
	if s.tokens == nil || s.tokens.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	input := &cognito.InitiateAuthInput{
		AuthFlow: types.AuthFlowTypeRefreshTokenAuth,
		ClientId: aws.String(s.clientID),
		AuthParameters: map[string]string{
			"REFRESH_TOKEN": s.tokens.RefreshToken,
		},
	}

	result, err := s.client.InitiateAuth(context.Background(), input)
	if err != nil {
		// Refresh token expired (>30 days) — user must re-login
		s.Logout()
		return fmt.Errorf("refresh token expired, please login again: %w", err)
	}

	if result.AuthenticationResult == nil {
		return fmt.Errorf("unexpected empty result from token refresh")
	}

	s.tokens.IDToken = *result.AuthenticationResult.IdToken
	s.tokens.AccessToken = *result.AuthenticationResult.AccessToken
	s.tokens.ExpiresAt = time.Now().Add(time.Duration(result.AuthenticationResult.ExpiresIn) * time.Second).Unix()

	// Refresh token is NOT returned on refresh — keep the existing one

	return s.saveTokensToKeyring()
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

// getStoredUsername returns the email for challenge responses.
func (s *Service) getStoredUsername() string {
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

// resolveEmployeeID calls GET /resolve-employee?employeeId=... to get the email.
// This endpoint does not require authentication.
func (s *Service) resolveEmployeeID(employeeID string) (string, error) {
	cfg := config.Get()
	url := fmt.Sprintf("%s/resolve-employee?employeeId=%s", cfg.APIURL, employeeID)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("employee ID not found (status %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
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
