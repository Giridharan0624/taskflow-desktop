//go:build windows

package auth

import (
	"encoding/base64"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows DPAPI — encrypts data using the current user's Windows credentials.
// Only the same Windows user account can decrypt it. Even if the keyring is
// extracted, the encrypted data is useless without the user's login.

var (
	crypt32                = windows.NewLazySystemDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	kernel32ForCrypto      = windows.NewLazySystemDLL("kernel32.dll")
	procLocalFree          = kernel32ForCrypto.NewProc("LocalFree")
)

// CRYPTPROTECT_UI_FORBIDDEN tells DPAPI to fail rather than pop a modal
// prompt when it would otherwise ask the user to confirm. Token
// encrypt/decrypt runs from background goroutines at startup and during
// token refresh — a modal there would deadlock the app. See M-AUTH-2.
const cryptprotectUIForbidden uintptr = 0x1

// DATA_BLOB is the Windows DPAPI data structure.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newDataBlob(data []byte) *dataBlob {
	if len(data) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{
		cbData: uint32(len(data)),
		pbData: &data[0],
	}
}

func (b *dataBlob) toBytes() []byte {
	d := make([]byte, b.cbData)
	copy(d, unsafe.Slice(b.pbData, b.cbData))
	return d
}

// encryptDPAPI encrypts data using Windows DPAPI (current user scope).
// Returns base64-encoded encrypted string.
func encryptDPAPI(plaintext string) (string, error) {
	input := newDataBlob([]byte(plaintext))
	var output dataBlob

	ret, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(input)),
		0, // no description
		0, // no additional entropy
		0, // reserved
		0, // no prompt
		cryptprotectUIForbidden,
		uintptr(unsafe.Pointer(&output)),
	)
	if ret == 0 {
		return "", fmt.Errorf("CryptProtectData failed: %w", err)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData)))

	encrypted := output.toBytes()
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// decryptDPAPI decrypts a base64-encoded DPAPI-encrypted string.
func decryptDPAPI(encoded string) (string, error) {
	encrypted, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}

	input := newDataBlob(encrypted)
	var output dataBlob

	ret, _, dErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(input)),
		0, // no description
		0, // no additional entropy
		0, // reserved
		0, // no prompt
		cryptprotectUIForbidden,
		uintptr(unsafe.Pointer(&output)),
	)
	if ret == 0 {
		return "", fmt.Errorf("CryptUnprotectData failed: %w", dErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(output.pbData)))

	return string(output.toBytes()), nil
}
