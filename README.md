# EnVault CLI

A native Go CLI for client-encrypted environment synchronization. No Node.js runtime is required. Build with Go 1.26 or install a verified native release from your team's public CLI repository.

```sh
go build -trimpath -o bin/envault ./cmd/envault
./bin/envault --help
```

Before publishing, change `github.com/envault/cli` in `go.mod` and internal imports to your actual public repository path. This workspace has no configured remote and does not imply ownership of that GitHub namespace.

## Daily workflow

Create your account and recovery kit in your team's dashboard first. Keep the vault password separate from GitHub sign-in.

```sh
envault login --api-url https://your-envault.example
envault init --api-url https://your-envault.example --project PROJECT_UUID
envault pull --env dev --file .env
# Edit your local .env, then:
envault push --env dev --file .env
envault run --env dev -- npm run dev
envault history --env dev
envault restore REVISION_UUID --env dev
envault logout
```

`init --name NAME` creates a new project using an unlocked vault. `.envault.json` contains only the application origin and project ID; exclude it and your `.env` files from Git. `pull --force` explicitly confirms replacement of an existing regular file. Pull refuses symlink output. Unix files use mode 0600; Windows output uses an owner/System DACL. Native platform CI checks builds and smoke execution.

Human tokens live in macOS Keychain, Windows Credential Manager, or Linux Secret Service. There is no plaintext-token fallback. Headless Linux needs an available keyring for human login; use a machine credential in CI. Vault passwords are prompted without echo for each encryption operation. `--password-stdin` accepts one password line explicitly; never pass passwords as command-line arguments.

Push uses the last successful pull/push base for that exact local file and remote. Stale bases conflict. When intentionally replacing a remote environment from an unrelated file, use `--overwrite-remote`; this deliberately discards the normal stale-file safeguard. Pulling to another file does not update the original file's base.

## Team access and recovery

```sh
envault members invite GITHUB_NUMERIC_ID
envault members accept INVITATION_TOKEN
envault members list
envault members grant USER_UUID --env staging --fingerprint 'independently verified fingerprint' --write
envault members remove USER_UUID
envault rotate --env staging
# Verify every displayed recipient out of band, then:
envault rotate --env staging --verified-recipients
envault members confirm-ownership
envault members role USER_UUID owner
envault recover --kit /secure/path/envault-recovery-kit.json
```

New invitations require explicit environment-key sharing after fingerprint verification. Removing access freezes affected writes until rotation. Regrants receive only current keys. Rotation cannot erase previously downloaded secrets; rotate actual external credentials when exposure matters.

## CI credentials

```sh
envault credentials create production-deploy --env prod --days 30
envault credentials list
envault credentials revoke CREDENTIAL_UUID
```

Save the displayed JSON as a protected `ENVAULT_MACHINE` CI variable. It contains an API credential and private decryption key; treat the complete bundle as a secret. Run `envault run -- your-command` in CI. The bundle is environment-scoped, read-only, expires, and is excluded from the child environment. Existing machine credentials receive replacement wrapped keys during rotation without changing the bundle.

## Verification and releases

```sh
go mod verify
go vet ./...
go test -race ./...
goreleaser check
goreleaser release --snapshot --clean --skip=publish,sign
```

The tag workflow verifies all native platforms, creates a **draft** release with binaries, protocol files, checksums, signed checksum material and GitHub build attestations. Configure branch protection and inspect the draft before publication. See [release instructions](docs/releases.md) and [protocol v1](protocol/crypto.md).

MIT licensed. This code has automated tests and an internal review; it has not undergone an independent cryptographic audit.
