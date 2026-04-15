//go:build darwin

package auth

import (
	"encoding/json"
	"fmt"

	"github.com/zalando/go-keyring"
)

// saveTokensToKeyring stores tokens directly in the macOS Keychain.
// No chunking needed — no size limit on macOS Keychain.
func (s *Service) saveTokensToKeyring() error {
	if s.tokens == nil {
		return fmt.Errorf("no tokens to save")
	}

	pairs := map[string]string{
		"id_token":      s.tokens.IDToken,
		"access_token":  s.tokens.AccessToken,
		"refresh_token": s.tokens.RefreshToken,
	}
	for key, val := range pairs {
		encrypted, err := encryptDPAPI(val) // no-op on macOS
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", key, err)
		}
		if err := keyring.Set(KeyringService, key, encrypted); err != nil {
			return fmt.Errorf("keyring set %s: %w", key, err)
		}
	}

	meta, _ := json.Marshal(tokenMeta{ExpiresAt: s.tokens.ExpiresAt})
	if err := keyring.Set(KeyringService, "meta", string(meta)); err != nil {
		return fmt.Errorf("keyring set meta: %w", err)
	}

	return nil
}

// loadTokensFromKeyring loads tokens from the macOS Keychain.
func (s *Service) loadTokensFromKeyring() (*Tokens, error) {
	idToken, err := keyring.Get(KeyringService, "id_token")
	if err != nil {
		return nil, fmt.Errorf("keyring get id_token: %w", err)
	}
	accessToken, err := keyring.Get(KeyringService, "access_token")
	if err != nil {
		return nil, fmt.Errorf("keyring get access_token: %w", err)
	}
	refreshToken, err := keyring.Get(KeyringService, "refresh_token")
	if err != nil {
		return nil, fmt.Errorf("keyring get refresh_token: %w", err)
	}

	idToken, _ = decryptDPAPI(idToken)         // no-op on macOS
	accessToken, _ = decryptDPAPI(accessToken)   // no-op on macOS
	refreshToken, _ = decryptDPAPI(refreshToken) // no-op on macOS

	metaStr, err := keyring.Get(KeyringService, "meta")
	if err != nil {
		return nil, fmt.Errorf("keyring get meta: %w", err)
	}
	var meta tokenMeta
	json.Unmarshal([]byte(metaStr), &meta)

	return &Tokens{
		IDToken:      idToken,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    meta.ExpiresAt,
	}, nil
}

// deleteTokensFromKeyring removes all stored tokens from the macOS
// Keychain. Returns the first error encountered; callers log or surface
// it rather than pretending logout always succeeded (H-AUTH-3).
func (s *Service) deleteTokensFromKeyring() error {
	var firstErr error
	for _, key := range []string{"id_token", "access_token", "refresh_token", "meta"} {
		if err := keyring.Delete(KeyringService, key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
