//go:build linux

package auth

import (
	"encoding/json"
	"fmt"

	"github.com/zalando/go-keyring"
)

// saveTokensToKeyring stores tokens directly in the Linux secret-service
// (GNOME Keyring / KDE Wallet). No chunking needed — no size limit on Linux.
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
		encrypted, err := encryptDPAPI(val) // no-op on Linux
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

// loadTokensFromKeyring loads tokens from the Linux secret-service.
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

	idToken, _ = decryptDPAPI(idToken)         // no-op on Linux
	accessToken, _ = decryptDPAPI(accessToken)   // no-op on Linux
	refreshToken, _ = decryptDPAPI(refreshToken) // no-op on Linux

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

// deleteTokensFromKeyring removes all stored tokens from the Linux secret-service.
func (s *Service) deleteTokensFromKeyring() {
	for _, key := range []string{"id_token", "access_token", "refresh_token", "meta"} {
		keyring.Delete(KeyringService, key)
	}
}
