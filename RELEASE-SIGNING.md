# Release signing (Ed25519)

Each release's `SHA256SUMS` file is (or can be) signed with an Ed25519
key. The binary already verifies its own SHA-256 against that file,
so a signed `SHA256SUMS` closes the remaining supply-chain gap: an
attacker who compromises the GitHub release (stolen deploy key, CI
takeover) can no longer publish a matching binary + hash pair,
because they can't also produce a valid Ed25519 signature without
the offline signing key.

Until the key is generated and the workflow step is enabled, releases
ship unsigned and the updater logs a warning but proceeds. See
`internal/updater/release_pubkey.go` for the flag that flips behaviour.

## One-time setup

1. **Generate an Ed25519 keypair offline** (a laptop that doesn't
   touch the build pipeline; the private seed must never appear in
   a build artifact or in git):

   ```bash
   python3 - <<'PY'
   from nacl.signing import SigningKey
   import base64
   k = SigningKey.generate()
   print("PRIVATE (base64, goes in GH secret):")
   print(base64.b64encode(k.encode()).decode())
   print()
   print("PUBLIC (base64, goes in release.pub):")
   print(base64.b64encode(k.verify_key.encode()).decode())
   PY
   ```

2. **Store the PRIVATE seed** in GitHub repo settings:
   Settings → Secrets and variables → Actions → New repository secret
   - Name: `RELEASE_SIGNING_KEY_B64`
   - Value: the PRIVATE base64 line from step 1.

3. **Commit the public key** to `desktop/internal/updater/release.pub`
   (paste just the base64 line, no `ssh-*` prefix, no header/footer).
   Trailing whitespace is fine — `signedReleases()` calls `strings.TrimSpace`.

4. **Uncomment the sign step** in `.github/workflows/release.yml`
   (search for "V2-H1"). Commit alongside the `release.pub` change.

5. **Verify the next tagged release** produces `SHA256SUMS.sig`
   alongside `SHA256SUMS` and that the updater log prints
   "SHA256SUMS signature verified against release public key".

## Day-to-day

There's nothing to do. Pushing a `v*` tag runs the release workflow,
which signs the checksum file with the GH secret and attaches
`SHA256SUMS.sig` to the release. The updater in every installed
client fetches both files and refuses to install if the signature
doesn't verify against the embedded public key.

## Key rotation

If the private key is ever suspected of compromise:

1. Generate a new pair (step 1 above).
2. Update the GH secret (step 2).
3. Commit the new public key (step 3).
4. Tag a new release.
5. **Installed clients running old versions will keep trusting the
   old public key until they update.** Their next "Update Now" will
   receive a signature signed with the NEW key, which the OLD
   embedded key will reject — making the update fail.
6. Users will need a manual installer for the rotation release.
   Distribute via email / Slack with an out-of-band SHA-256 check.

Key rotation is rare and painful. Keep the private key offline, use
GitHub Environment protections on the `RELEASE_SIGNING_KEY_B64`
secret, and you shouldn't need to rotate often.
