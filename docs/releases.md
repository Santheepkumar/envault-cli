# Publish, verify, install and withdraw native releases

This repository publishes the `envault` binary for macOS, Linux and Windows on amd64 and arm64. Homebrew and npm distribution are deferred. Signing configuration is present; a real signed release has not been published or verified from this workspace.

## 1. Configure the actual public repository

The public module and repository are `github.com/Santheepkumar/envault-cli` and `Santheepkumar/envault-cli`. Before the first public release, enable private vulnerability reporting, branch protection and required native CI checks. Never publish the sibling private application repository or its configuration/secrets.

Configure GitHub Actions with read-only default permissions and enable OIDC/attestations for the release job. The checked-in workflow explicitly grants `contents:write`, `id-token:write` and `attestations:write` to that job. It uses `GITHUB_TOKEN`; no long-lived publication token is required for the repository's own releases. Set an environment review rule if your organization requires publication approval.

The private application CI checks out this public repository at a reviewed commit SHA. Update that SHA after each approved CLI change, and update the pinned protocol snapshot only when the protocol changes. Compatibility checks must pass without access to private application source from the public CLI workflow.

## 2. Validate a release candidate

Use the Go toolchain pinned in `go.mod` (currently 1.26.10) and GoReleaser 2.18.3, matching the workflow. From the CLI repository:

```sh
go mod verify
go vet ./...
go test -race ./...
go build -trimpath -o bin/envault ./cmd/envault
./bin/envault --version
./bin/envault run --help
goreleaser check
goreleaser release --snapshot --clean --skip=publish,sign
```

On Windows use `-o bin/envault.exe` and invoke that file. Snapshot builds do not publish or prove signing works. They require Git metadata and the actual configured remote. Inspect `dist/` for six platform/architecture archives and `checksums.txt`. Snapshot output is ignored by Git. Do not mark unsupported architectures verified just because they cross-compile.

Review the release diff, changelog, security findings and application protocol compatibility. Require all native CI matrix jobs to pass. Before tagging, confirm the exact commit is clean and reviewed:

```sh
git status --short
git log -1 --oneline
git tag --list v0.1.0
```

The example version below is illustrative: choose the next unused semantic version. Reusing a published version is prohibited.

## 3. Create and inspect the signed draft

These commands publish Git state and trigger the release workflow; run them only when authorized for the actual repository:

```sh
git tag -a v0.1.0 -m 'EnVault v0.1.0'
git push origin v0.1.0
gh run list --workflow release.yml --limit 5
gh release view v0.1.0 --repo Santheepkumar/envault-cli
```

The tag workflow first runs native checks, then GoReleaser creates a **draft** containing archives, checksum signatures/certificates and protocol documentation. GitHub also records build attestations. Check that the draft commit equals the reviewed tag and that there are no unexpected assets. If any job fails, keep the draft unpublished and diagnose the failure; do not bypass native/security gates.

Download the candidate assets into an empty directory. Draft asset access requires an authenticated maintainer:

```sh
gh release download v0.1.0 --repo Santheepkumar/envault-cli --dir candidate
```

## 4. Verify assets before installation

Install `cosign` and GitHub CLI through their official channels. Use the actual repository, exact tag and workflow identity. From `candidate/`:

```sh
cosign verify-blob checksums.txt --signature checksums.txt.sig \
  --certificate checksums.txt.pem \
  --certificate-identity 'https://github.com/Santheepkumar/envault-cli/.github/workflows/release.yml@refs/tags/v0.1.0' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check checksums.txt
gh attestation verify ./envault_0.1.0_linux_amd64.tar.gz --repo Santheepkumar/envault-cli
```

Checksum verification must follow successful signature verification. Run attestation verification for each archive being distributed. Verify its subject digest and repository/workflow identity. On macOS use `shasum -a 256 -c checksums.txt`; on Windows compare `Get-FileHash -Algorithm SHA256 .\envault_0.1.0_windows_amd64.zip` with that archive's exact entry in the authenticated checksum file. A failure blocks installation and publication.

Archive checksum signatures are not macOS notarization or Windows Authenticode signatures. Those programs require separately configured signing identities. Never tell users to disable system security globally to install an artifact.

## 5. Install and test the actual binary

Choose your OS and architecture (`uname -m` on Unix; `$env:PROCESSOR_ARCHITECTURE` on Windows). macOS/Linux x86_64 uses `amd64`; aarch64/arm64 uses `arm64`. Example Linux user installation after verification:

```sh
mkdir unpacked
tar -xzf envault_0.1.0_linux_amd64.tar.gz -C unpacked
mkdir -p "$HOME/.local/bin"
install -m 0755 unpacked/envault "$HOME/.local/bin/envault"
"$HOME/.local/bin/envault" --version
```

macOS uses the equivalent `darwin` archive and can use the same user installation directory. Add `$HOME/.local/bin` to your shell PATH if needed. On Windows:

```powershell
Expand-Archive .\envault_0.1.0_windows_amd64.zip -DestinationPath .\unpacked
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\EnVault\bin"
Copy-Item .\unpacked\envault.exe "$env:LOCALAPPDATA\EnVault\bin\envault.exe"
& "$env:LOCALAPPDATA\EnVault\bin\envault.exe" --version
```

Add that directory to your user PATH using Windows environment settings, then open a new terminal. Avoid system-wide installation until the candidate is approved.

On real macOS, Linux and Windows hosts, use synthetic staging data to verify browser-approved login and the native OS keychain, vault unlock, push/pull byte equality, restrictive output permissions, run arguments/streams/exit codes and interruption of a child with descendants. Unix signal exits use `128+signal`; after five seconds of graceful shutdown EnVault kills the invocation's remaining process tree. Windows uses a console process group and a kill-on-close Job Object. Linux human login requires a running Secret Service in the user's session; headless CI should use a scoped machine bundle.

Native CI now includes process-tree cancellation regressions. Cross-compilation on one OS is not evidence that the others executed. Record host, architecture, artifact checksum, command results and staging revision in the release checklist.

## 6. Publish and pin

After signature/provenance and all native staging checks pass, inspect release notes and assets once more, then explicitly publish the reviewed draft:

```sh
gh release edit v0.1.0 --repo Santheepkumar/envault-cli --draft=false
```

Publish only the same assets already verified. Update installation documentation and the private application's pinned CLI release/reference; run compatibility CI before deploying the app. Keep previous valid artifacts available for rollback.

## Withdrawal and incident response

If a candidate fails before publication, leave it as a draft and fix forward with a fresh candidate/version. If a public release is unsafe, immediately mark it deprecated/unsafe in its release notes, remove its latest-release status and publish a corrected version. Notify affected operators through an authorized channel. Never silently replace an archive or move/reuse the tag: checksums and provenance must remain tied to immutable history.

For an active compromise, first preserve the workflow logs, signed checksum material, tag and commit as incident evidence. Revoke exposed credentials, restrict release permissions, and hide or remove compromised distribution assets when necessary. Document exactly what was withdrawn and the safe replacement; restore service with a newly reviewed tag and fresh signed artifacts.

## Reproducibility and current verification

Builds disable CGO, trim paths, pin module sums, use commit timestamps and version-only linker metadata. Rebuild with the identical Go toolchain and tag; compare binary bytes, not compressed archive bytes. Cross-platform signing and real keychain/staging smoke checks require configured accounts and native hosts and remain release gates.

Local validation of this readiness change: macOS Go race tests and encrypted file-byte regressions pass; Windows tests compile but have not executed here. Publication/tagging commands in this guide have not been executed. See the application verification record for the combined acceptance status.

Official references: [GoReleaser checks](https://goreleaser.com/cmd/goreleaser_check/), [snapshot releases](https://goreleaser.com/customization/snapshots/), [Windows Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects), and [console process groups](https://learn.microsoft.com/en-us/windows/console/console-process-groups).

## Onboarding rollout

The onboarding update changes fresh CLI installations from localhost to the hosted origin `https://d3z7pylxp15sx.cloudfront.net`. Local and self-hosted users supply `login --api-url ORIGIN` once; existing project bindings retain their origin. The protocol and machine bundle format are unchanged.

Publish and verify the native CLI release before deploying dashboard quick-reference instructions that omit repeated URLs. After the reviewed CLI commit is available in the public repository, update the private application's pinned CLI reference in `.github/workflows/ci.yml` to that exact commit and run application compatibility CI. Do not point the pin at an unpublished or guessed commit. Source changes alone do not establish native browser/keychain verification or release readiness.
