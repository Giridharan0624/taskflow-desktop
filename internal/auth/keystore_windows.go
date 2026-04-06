//go:build windows

package auth

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	keyIdToken      = "id_token"
	keyAccessToken  = "access_token"
	keyRefreshToken = "refresh_token"
	keyMeta         = "meta"

	// Windows Credential Manager has a ~2500 byte limit per entry.
	// DPAPI + base64 can double the token size, so we split into chunks.
	maxChunkSize = 2000
)

// saveTokensToKeyring encrypts tokens with DPAPI then stores in OS keychain.
// Large tokens are split into multiple entries to stay under the Windows limit.
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
		if err := saveChunked(key, encrypted); err != nil {
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
		encrypted, err := loadChunked(key)
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
	deleteChunked(keyIdToken)
	deleteChunked(keyAccessToken)
	deleteChunked(keyRefreshToken)
	_ = keyring.Delete(KeyringService, keyMeta)
}

// saveChunked splits a value into chunks and stores each one.
func saveChunked(key, value string) error {
	if len(value) <= maxChunkSize {
		// Fits in a single entry
		_ = keyring.Delete(KeyringService, key+"_1") // Clean up old chunks
		return keyring.Set(KeyringService, key, value)
	}

	// Split into chunks
	chunks := splitString(value, maxChunkSize)

	// Store chunk count in the main key
	if err := keyring.Set(KeyringService, key, fmt.Sprintf("CHUNKED:%d", len(chunks))); err != nil {
		return err
	}

	for i, chunk := range chunks {
		chunkKey := fmt.Sprintf("%s_%d", key, i)
		if err := keyring.Set(KeyringService, chunkKey, chunk); err != nil {
			return fmt.Errorf("chunk %d: %w", i, err)
		}
	}
	return nil
}

// loadChunked reads a potentially chunked value.
func loadChunked(key string) (string, error) {
	value, err := keyring.Get(KeyringService, key)
	if err != nil {
		return "", err
	}

	// Check if it's a chunked value
	if !strings.HasPrefix(value, "CHUNKED:") {
		return value, nil
	}

	var count int
	fmt.Sscanf(value, "CHUNKED:%d", &count)
	if count <= 0 || count > 20 {
		return "", fmt.Errorf("invalid chunk count: %d", count)
	}

	var builder strings.Builder
	for i := 0; i < count; i++ {
		chunkKey := fmt.Sprintf("%s_%d", key, i)
		chunk, err := keyring.Get(KeyringService, chunkKey)
		if err != nil {
			return "", fmt.Errorf("chunk %d missing: %w", i, err)
		}
		builder.WriteString(chunk)
	}
	return builder.String(), nil
}

// deleteChunked removes a key and all its chunks.
func deleteChunked(key string) {
	value, _ := keyring.Get(KeyringService, key)
	_ = keyring.Delete(KeyringService, key)

	if strings.HasPrefix(value, "CHUNKED:") {
		var count int
		fmt.Sscanf(value, "CHUNKED:%d", &count)
		for i := 0; i < count; i++ {
			_ = keyring.Delete(KeyringService, fmt.Sprintf("%s_%d", key, i))
		}
	}
	// Also clean up any stale chunk keys
	for i := 0; i < 10; i++ {
		_ = keyring.Delete(KeyringService, fmt.Sprintf("%s_%d", key, i))
	}
}

func splitString(s string, size int) []string {
	var chunks []string
	for len(s) > 0 {
		if len(s) < size {
			size = len(s)
		}
		chunks = append(chunks, s[:size])
		s = s[size:]
	}
	return chunks
}
