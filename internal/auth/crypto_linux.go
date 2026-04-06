//go:build linux

package auth

// encryptDPAPI is a no-op on Linux. The secret-service (GNOME Keyring / KDE Wallet)
// encrypts stored credentials at rest, so application-level encryption is unnecessary.
func encryptDPAPI(plaintext string) (string, error) {
	return plaintext, nil
}

// decryptDPAPI is a no-op on Linux.
func decryptDPAPI(encoded string) (string, error) {
	return encoded, nil
}
