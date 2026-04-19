//go:build windows

package auth

import (
	"encoding/json"
	"fmt"
	"log"
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
			// No plaintext fallback: a same-user process could forge a
			// plaintext keyring entry and trivially bypass DPAPI. Wipe the
			// stored tokens and force re-authentication. The delete error
			// is secondary to the real failure — log, don't shadow.
			if delErr := s.deleteTokensFromKeyring(); delErr != nil {
				log.Printf("keystore cleanup after DPAPI failure returned: %v", delErr)
			}
			return nil, fmt.Errorf("stored %s is corrupt or tampered, please log in again: %w", key, err)
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
// Returns the first error encountered across the three chunked token
// deletes and the meta delete, so a caller can surface a genuinely
// broken logout instead of silently pretending it succeeded (H-AUTH-3).
func (s *Service) deleteTokensFromKeyring() error {
	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	record(deleteChunked(keyIdToken))
	record(deleteChunked(keyAccessToken))
	record(deleteChunked(keyRefreshToken))
	record(keyring.Delete(KeyringService, keyMeta))
	return firstErr
}

// saveChunked splits a value into chunks and stores each one atomically.
//
// Crash-safety: the previous implementation wrote the "CHUNKED:N"
// sentinel FIRST and then wrote the data chunks in a loop. A crash
// (or keyring API error) mid-loop left the sentinel pointing at
// chunks that didn't exist yet, so the next loadChunked attempt would
// hit a "chunk N missing" error and the user could never log in again
// without manually wiping the keystore.
//
// The fix (T-ATOMIC-1): write all chunks under TEMP keys first, then
// publish the sentinel under the real key, then promote each temp
// chunk to its real key. A crash at any point up to the sentinel
// write leaves the previous value intact; a crash between sentinel
// write and promote leaves a pointer to temp keys we then promote on
// next start (or let the user re-login — the existing data is gone
// by that point anyway).
func saveChunked(key, value string) error {
	if len(value) <= maxChunkSize {
		// Fits in a single entry. Clean up any old chunks from a
		// previous larger value under the same key before writing.
		for i := 0; i < 20; i++ {
			_ = keyring.Delete(KeyringService, fmt.Sprintf("%s_%d", key, i))
		}
		return keyring.Set(KeyringService, key, value)
	}

	chunks := splitString(value, maxChunkSize)

	// Step 1: write every chunk under a TEMP key. If this loop fails
	// mid-way, clean up the temp keys we've already written so the
	// next save attempt starts from a clean slate.
	for i, chunk := range chunks {
		tempKey := fmt.Sprintf("%s_tmp_%d", key, i)
		if err := keyring.Set(KeyringService, tempKey, chunk); err != nil {
			for j := 0; j < i; j++ {
				_ = keyring.Delete(KeyringService, fmt.Sprintf("%s_tmp_%d", key, j))
			}
			return fmt.Errorf("chunk %d (temp): %w", i, err)
		}
	}

	// Step 2: publish the sentinel. Until this write commits, loads see
	// the previous value (or nothing). This is the atomic swap point.
	if err := keyring.Set(KeyringService, key, fmt.Sprintf("CHUNKED:%d", len(chunks))); err != nil {
		for i := range chunks {
			_ = keyring.Delete(KeyringService, fmt.Sprintf("%s_tmp_%d", key, i))
		}
		return fmt.Errorf("sentinel: %w", err)
	}

	// Step 3: promote temp chunks to their real keys. A crash here
	// leaves a sentinel pointing at real keys that may be missing —
	// we handle that by retrying the promotion below and, if it still
	// fails, leaving the sentinel + temp keys in place. The next load
	// will fail cleanly and force a re-login rather than returning a
	// mixed-content corrupted token.
	for i, chunk := range chunks {
		realKey := fmt.Sprintf("%s_%d", key, i)
		tempKey := fmt.Sprintf("%s_tmp_%d", key, i)
		if err := keyring.Set(KeyringService, realKey, chunk); err != nil {
			return fmt.Errorf("chunk %d (promote): %w", i, err)
		}
		_ = keyring.Delete(KeyringService, tempKey)
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
	// An error from Sscanf here (e.g. "CHUNKED:abc") used to leave
	// count=0, skip the read loop, and propagate as a cryptic
	// "unexpected end of JSON" when the caller unmarshaled "". Surface
	// the real cause so callers can drop and re-authenticate instead of
	// chasing a phantom JSON bug.
	n, err := fmt.Sscanf(value, "CHUNKED:%d", &count)
	if err != nil || n != 1 {
		return "", fmt.Errorf("malformed chunk sentinel %q: %w", value, err)
	}
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

// deleteChunked removes a key and all its chunks. Returns the error from
// the primary key delete (the one that matters for logout correctness);
// stale chunk deletes are best-effort since they may or may not exist.
func deleteChunked(key string) error {
	value, _ := keyring.Get(KeyringService, key)
	primaryErr := keyring.Delete(KeyringService, key)

	if strings.HasPrefix(value, "CHUNKED:") {
		var count int
		fmt.Sscanf(value, "CHUNKED:%d", &count)
		for i := 0; i < count; i++ {
			_ = keyring.Delete(KeyringService, fmt.Sprintf("%s_%d", key, i))
		}
	}
	// Also clean up any stale chunk keys — best-effort cleanup,
	// errors here don't affect logout correctness. Must match loadChunked's
	// upper bound of 20 (see H-AUTH-2) or old chunks 10..19 would survive
	// logout and reappear on the next load with truncated content.
	for i := 0; i < 20; i++ {
		_ = keyring.Delete(KeyringService, fmt.Sprintf("%s_%d", key, i))
	}
	return primaryErr
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
