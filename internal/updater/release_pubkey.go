package updater

import _ "embed"

// releasePubKeyBase64 is the Ed25519 public key that signs each
// release's SHA256SUMS file. Embedded at build time from
// release.pub so a compromised update CDN / GitHub release can't
// smuggle a modified SHA256SUMS past the integrity check:
// SHA256SUMS verifies the binary, this signature verifies
// SHA256SUMS.
//
// An empty value disables signature verification — the updater
// still checks the binary's SHA-256 against the SHA256SUMS file,
// but the file itself is trusted. Useful during the bootstrap
// period before the release key is generated and the workflow is
// updated to sign.
//
// To activate signed releases:
//  1. Generate an Ed25519 keypair offline (see docs/RELEASE-GUIDE.md).
//  2. Paste the base64 public key into release.pub in this folder.
//  3. Store the private key as the GitHub secret
//     RELEASE_SIGNING_KEY_B64.
//  4. Uncomment the sign step in .github/workflows/release.yml.
//
// See V2-H1.
//
//go:embed release.pub
var releasePubKeyBase64 string
