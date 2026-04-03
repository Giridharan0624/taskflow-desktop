package auth

import (
	"encoding/json"
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	keyIdToken      = "id_token"
	keyAccessToken  = "access_token"
	keyRefreshToken = "refresh_token"
	keyMeta         = "meta"
)

type tokenMeta struct {
	ExpiresAt int64 `json:"expiresAt"`
}

// saveTokensToKeyring encrypts tokens with DPAPI then stores in OS keychain.
// Double protection: DPAPI encryption (user-scoped) + Windows Credential Manager.
func (s *Service) saveTokensToKeyring() error {
	entries := map[string]string{
		keyIdToken:      s.tokens.IDToken,
		keyAccessToken:  s.tokens.AccessToken,
		keyRefreshToken: s.tokens.RefreshToken,
	}

	for key, val := range entries {
		encrypted, err := encryptDPAPI(val)
		if err != nil {
			return fmt.Errorf("failed to encrypt %s: %w", key, err)
		}
		if err := keyring.Set(KeyringService, key, encrypted); err != nil {
			return fmt.Errorf("failed to save %s to keyring: %w", key, err)
		}
	}

	meta, _ := json.Marshal(tokenMeta{ExpiresAt: s.tokens.ExpiresAt})
	if err := keyring.Set(KeyringService, keyMeta, string(meta)); err != nil {
		return fmt.Errorf("failed to save meta to keyring: %w", err)
	}

	return nil
}

// loadTokensFromKeyring reads and decrypts tokens from the OS keychain.
func (s *Service) loadTokensFromKeyring() (*Tokens, error) {
	keys := []string{keyIdToken, keyAccessToken, keyRefreshToken}
	decrypted := make(map[string]string)

	for _, key := range keys {
		encrypted, err := keyring.Get(KeyringService, key)
		if err != nil {
			return nil, fmt.Errorf("no stored %s: %w", key, err)
		}
		plain, err := decryptDPAPI(encrypted)
		if err != nil {
			// Fallback: might be unencrypted (from older version)
			plain = encrypted
		}
		decrypted[key] = plain
	}

	metaStr, err := keyring.Get(KeyringService, keyMeta)
	if err != nil {
		return nil, fmt.Errorf("no stored meta: %w", err)
	}

	var meta tokenMeta
	if err := json.Unmarshal([]byte(metaStr), &meta); err != nil {
		return nil, fmt.Errorf("failed to parse meta: %w", err)
	}

	return &Tokens{
		IDToken:      decrypted[keyIdToken],
		AccessToken:  decrypted[keyAccessToken],
		RefreshToken: decrypted[keyRefreshToken],
		ExpiresAt:    meta.ExpiresAt,
	}, nil
}

// deleteTokensFromKeyring removes all tokens from the OS keychain.
func (s *Service) deleteTokensFromKeyring() {
	_ = keyring.Delete(KeyringService, keyIdToken)
	_ = keyring.Delete(KeyringService, keyAccessToken)
	_ = keyring.Delete(KeyringService, keyRefreshToken)
	_ = keyring.Delete(KeyringService, keyMeta)
}
