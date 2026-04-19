package updater

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"taskflow-desktop/internal/security"
)

// SignatureAssetName is the name of the detached Ed25519 signature
// asset that signs SHA256SUMS. Kept next to ChecksumAssetName because
// the pair is meaningless apart — one signs, the other is signed.
const SignatureAssetName = "SHA256SUMS.sig"

// signedReleases reports whether a release public key is embedded in
// this binary. When it returns false the updater still verifies the
// SHA-256 of the downloaded binary against SHA256SUMS — it just
// doesn't verify that SHA256SUMS itself was signed by the release
// key. Keep this check at one site so the bootstrap period ("no key
// yet") and the steady state ("signed releases required") flip
// together.
func signedReleases() bool {
	return strings.TrimSpace(releasePubKeyBase64) != ""
}

// verifySignature checks that the detached Ed25519 signature is a
// valid signature of the given body under the embedded release
// public key. Returns nil on success; wrapped errors on any kind of
// failure (bad key format, malformed signature, verification failed).
//
// Body here is expected to be the raw bytes of SHA256SUMS as
// uploaded to the release. The signature is base64-encoded (so it
// fits in an ASCII file and survives URL encoding).
func verifySignature(body, signatureB64 []byte) error {
	keyRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(releasePubKeyBase64))
	if err != nil {
		return fmt.Errorf("release pubkey base64: %w", err)
	}
	if len(keyRaw) != ed25519.PublicKeySize {
		return fmt.Errorf("release pubkey wrong length: got %d want %d", len(keyRaw), ed25519.PublicKeySize)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signatureB64)))
	if err != nil {
		return fmt.Errorf("signature base64: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("signature wrong length: got %d want %d", len(sig), ed25519.SignatureSize)
	}
	if !ed25519.Verify(ed25519.PublicKey(keyRaw), body, sig) {
		return fmt.Errorf("signature does not verify against release public key")
	}
	return nil
}

// fetchSignature downloads the detached signature file for
// SHA256SUMS, validating the URL against allowedUpdateHosts. Returns
// the raw (base64-encoded) signature bytes so the caller can pair
// them with a freshly-fetched SHA256SUMS body.
func fetchSignature(rawURL string) ([]byte, error) {
	if _, err := security.ValidateHTTPSURL(rawURL, allowedUpdateHosts); err != nil {
		return nil, fmt.Errorf("signature URL rejected: %w", err)
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	// Short-lived client — the signature file is a few hundred bytes.
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d fetching signature", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4*1024))
}
