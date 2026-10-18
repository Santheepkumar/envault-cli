# Native release operations

1. Configure the actual public GitHub remote and module path; enable branch protection and private vulnerability reporting.
2. Run native CI on macOS, Linux and Windows, then review protocol compatibility with the private application. Update the changelog for the release.
3. Push an annotated semantic-version tag only after checks pass. The release workflow creates a draft, uses keyless Cosign signing, and records GitHub build provenance.
4. Download each platform archive, verify its checksum and signature, extract it, run `envault --version`, and perform login/pull/run against staging on actual supported platforms. Cross-compilation alone is insufficient.
5. Inspect and publish the draft. Record its tag in the private application's pinned protocol/CI configuration.

For a release hosted by `OWNER/REPOSITORY`, verify the checksum signature using its exact workflow identity and tag:

```sh
cosign verify-blob checksums.txt --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-identity 'https://github.com/OWNER/REPOSITORY/.github/workflows/release.yml@refs/tags/v0.1.0' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check checksums.txt
gh attestation verify ./envault_ARCHIVE --repo OWNER/REPOSITORY
```

Use `shasum -a 256` on macOS or `Get-FileHash -Algorithm SHA256` on Windows if `sha256sum` is unavailable. Install only the archive whose checksum you verified. Archive signatures authenticate the checksums; they are not macOS notarization or Windows Authenticode signatures. Those platform trust programs require separately configured signing identities.

Reproducible binary settings: pinned Go module sums, no CGO, trimmed paths, commit-based timestamps and version-only linker metadata. Rebuild with the same Go toolchain and release tag; compare binary bytes rather than compressed archive bytes.
