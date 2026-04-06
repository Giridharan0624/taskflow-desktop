//go:build darwin

package auth

// encryptDPAPI is a no-op on macOS. The Keychain encrypts stored
// credentials at rest, so application-level encryption is unnecessary.
func encryptDPAPI(plaintext string) (string, error) {
	return plaintext, nil
}

// decryptDPAPI is a no-op on macOS.
func decryptDPAPI(encoded string) (string, error) {
	return encoded, nil
}
