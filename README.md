# EnVault CLI

A native Go CLI for client-encrypted environment synchronization. No Node.js runtime is required. Build with Go 1.26.10 or install a verified native release from your team's public CLI repository.

```sh
go build -trimpath -o bin/envault ./cmd/envault
./bin/envault --help
```

The public Go module is `github.com/Santheepkumar/envault-cli`. Build or install releases from that repository.

Application operators should follow the private application repository’s local setup and testing guides. This public repository’s [release guide](docs/releases.md) is self-contained for binary verification and installation.

For complete workflows, multi-project examples, every command and flag, team access, recovery, and CI, read the [CLI user guide](docs/user-guide.md).

## Daily workflow

Create your account and recovery kit in your team's dashboard first. Keep the vault password separate from GitHub sign-in.

```sh
envault login
envault init
envault status
envault pull --env dev --file .env
# Edit your local .env, then:
envault push --env dev --file .env
envault run --env dev -- npm run dev
envault projects --limit 20
envault history --env dev
# Continue history using its numeric nextCursor:
envault history --env dev --before 42
envault restore REVISION_UUID --env dev
envault logout
```

`login` defaults to the hosted installation at `https://d3z7pylxp15sx.cloudfront.net`, opens your browser, and remembers the installation after approval. Use `login --no-browser` to open the printed approval URL yourself. For a self-hosted installation, supply the URL once:

```sh
envault login --api-url https://your-envault.example
envault init
```

`init` offers a project picker, including an option to create a project. Selecting an existing project does not require your vault password. Scripts can use `init --project PROJECT_UUID` or `init --name NAME`; creating a project unlocks the vault. These flags are mutually exclusive. Repeating `init` for the same binding is harmless. To choose a different project, run `init --force` interactively, or use `init --project PROJECT_UUID --force` in scripts. Relinking from a subdirectory updates the nearest existing binding.

Commands find the nearest `.envault.json` in the current directory or its parents. File arguments still resolve relative to your current directory. The installation comes from `--api-url`, then the project binding, then the remembered user installation, then the hosted default. Project commands reject a conflicting `--api-url` unless `--project` is also supplied explicitly. Use `init` to save a new binding. Local development requires `login --api-url http://127.0.0.1:3000` once.

`status` checks authentication without unlocking your vault and reports the installation, configuration source, and project. Machine credentials report their configured installation and scope without a remote authentication check. Logout removes the selected token but remembers the installation address. The user configuration at the OS user-config directory under `envault/config.json` contains only `apiUrl`, never tokens or keys.

`.envault.json` contains only the application origin and project ID; exclude it and your `.env` files from Git. `pull --force` explicitly confirms replacement of an existing regular file. Pull refuses symlink output. Unix files use mode 0600; Windows output uses an owner/System DACL. Native platform CI checks builds, process-tree cancellation and smoke execution. Synchronization preserves CRLF, BOM, multiline values and trailing newlines byte for byte; process injection separately parses supported dotenv syntax.

Human tokens live in macOS Keychain, Windows Credential Manager, or Linux Secret Service. There is no plaintext-token fallback. Headless Linux needs an available keyring for human login; use a machine credential in CI. Vault passwords are prompted without echo for each encryption operation. `--password-stdin` accepts one password line explicitly; never pass passwords as command-line arguments.

Push uses the last successful pull/push base for that exact local file and remote. Stale bases conflict. When intentionally replacing a remote environment from an unrelated file, use `--overwrite-remote`; this deliberately discards the normal stale-file safeguard. Pulling to another file does not update the original file's base.

## Team access and recovery

```sh
envault members invite GITHUB_NUMERIC_ID
envault members accept INVITATION_TOKEN
envault members list --limit 20
# Continue with the UUID nextCursor, if present:
envault members list --before MEMBER_CURSOR_UUID --limit 20
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
envault credentials list --limit 20
envault credentials revoke CREDENTIAL_UUID
```

Save the displayed JSON as a protected `ENVAULT_MACHINE` CI variable. It contains an API credential and private decryption key; treat the complete bundle as a secret. Run `envault run -- your-command` in CI. Lost-device human CLI sessions can be revoked from the dashboard account controls; the current CLI token can revoke itself with `logout`. The bundle is environment-scoped, read-only, expires, and is excluded from the child environment. Existing machine credentials receive replacement wrapped keys during rotation without changing the bundle.

`run` forwards interruption to the process group and allows five seconds for graceful shutdown before terminating remaining descendants. It preserves ordinary child status and uses `128+signal` for POSIX signal exits. Windows compares injected variable names without case and rejects ambiguous duplicates.

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
