# EnVault CLI user guide

Use EnVault to share encrypted environment files across project directories, teammates, and deployment jobs. This guide explains the commands through day-to-day workflows, then provides a complete command and flag reference.

**Applies to:** protocol v1 and the current CLI source as of 24 October 2026, including remembered installations, interactive `init`, and `status`. These onboarding changes require a rebuilt CLI or a release containing them; an older installed binary may still require explicit URLs and project IDs. Check `envault --version` and `envault init --help`. This guide does not assert that a native release has been published.

Examples use Bash/zsh for directory changes, environment variables, and file operations. The `envault` commands also work on Windows; use the equivalent PowerShell file and environment-variable operations. Replace uppercase placeholders such as `PROJECT_UUID` with actual values. Commands that publish, restore, remove, or rotate data are intentional operations, not commands to run merely to try the guide.

## Contents

1. [Before you start](#before-you-start)
2. [First login and project setup](#first-login-and-project-setup)
3. [Working with multiple projects](#working-with-multiple-projects)
4. [Upload, download, and run](#upload-download-and-run)
5. [Conflicts, history, and restoration](#conflicts-history-and-restoration)
6. [Team access and ownership](#team-access-and-ownership)
7. [CI and machine credentials](#ci-and-machine-credentials)
8. [Recovery and logout](#recovery-and-logout)
9. [Complete command reference](#complete-command-reference)
10. [Configuration, troubleshooting, and related documents](#configuration-troubleshooting-and-related-documents)

## Before you start

You need a native EnVault CLI on your PATH and access to your team's dashboard. Use a verified native release from the public CLI repository; installation, checksums, signatures, and platform prerequisites are covered in the [release and installation guide](releases.md). No Node.js runtime is required for the CLI itself. A command launched through `envault run` needs its own runtime installed.

For a source build, run this from the CLI repository using the Go toolchain pinned in `go.mod`:

```sh
go build -trimpath -o bin/envault ./cmd/envault
./bin/envault --help
```

Use `./bin/envault` in place of `envault` until that binary is installed on your PATH. On Windows build `bin/envault.exe`.

In the dashboard:

1. Sign in with GitHub.
2. Create your vault with a separate vault password of at least 12 characters.
3. Download the recovery kit and confirm that you saved it securely.
4. Create a project, or accept an invitation and obtain environment access from an owner/admin. You can also create projects from the CLI after vault onboarding.

GitHub sign-in identifies you. The vault password decrypts your encryption identity locally. A successful login alone does not unlock your secrets. Keep the recovery kit separately from your development machine; it contains a decryption credential.

Add local files to your project's `.gitignore`:

```gitignore
.env
.env.*
!.env.example
.envault.json
```

Only commit a sanitized `.env.example`. Never put actual credentials in it. The CLI does not edit `.gitignore` for you.

## First login and project setup

### Hosted installation

From your project's root directory:

```sh
cd ~/projects/storefront
envault login
envault init
envault status
```

`login` uses `https://envault.strawhatdevs.com` for a fresh installation. In a terminal it opens the browser, prints the approval URL and code, and waits for you to approve the matching code. The approval request expires after 10 minutes. After approval, the CLI stores a 30-day human token in the OS keychain and remembers the installation address.

If browser opening is unavailable, open the printed URL yourself. To prevent automatic opening:

```sh
envault login --no-browser
```

`init` displays accessible projects with names and IDs. Select a number, choose `n` for the next page when offered, `c` to create a project, or `q` to cancel. Selecting an existing project does not ask for your vault password; creating one does. Duplicate project names are distinguished by ID.

`init` writes `.envault.json` with the installation origin and project ID. Repeating `init` for the same binding is harmless. `status` shows the resolved installation, its configuration source, the selected project, and human authentication state without unlocking your vault.

### Self-hosted installation or local development

Supply your installation origin during the first successful login:

```sh
envault login --api-url https://vault.example.com
envault init
```

For a local development application:

```sh
envault login --api-url http://127.0.0.1:3000
envault init
```

Use the application origin only: no `/login`, `/api/v1`, query string, or GitHub URL. HTTPS is required except for loopback development hosts. Remembering this address does not weaken encryption.

Existing project bindings take precedence over the remembered user address. If you work with several installations, log in to each as needed and keep each directory linked to the correct installation. Credentials are scoped to the origin; moving an installation to a new hostname requires login and deliberate relinking.

### Explicit setup for scripts

```sh
envault init --project PROJECT_UUID
envault init --name "new-service"
```

Choose one of `--project` or `--name`; they are mutually exclusive. Explicit project linking checks your access before writing configuration. Creation generates a project with empty `dev`, `staging`, and `prod` environments. In a noninteractive process, supply one of these flags rather than expecting a picker. Human login still needs an OS keychain, and project creation still needs a vault password.

## Working with multiple projects

### Separate directories: the normal workflow

Link each application once:

```sh
cd ~/projects/storefront
envault init
# Select Storefront

cd ~/projects/payments-api
envault init
# Select Payments API
```

Then changing directories changes the project automatically:

```sh
cd ~/projects/storefront
envault status
envault run -- npm run dev

cd ~/projects/payments-api
envault status
envault run -- go run ./cmd/server
```

There is no global `switch` command. The nearest `.envault.json`, found by searching upward from the current directory, determines the saved project. This also works from a subdirectory:

```sh
cd ~/projects/storefront/src
envault status
```

File paths still use your current directory. `envault pull` from `src/` writes `src/.env`, not the project-root `.env`. Prefer running file synchronization at the project root or pass the intended relative file path explicitly.

### Temporarily use another project

```sh
envault projects --limit 20
envault pull --project PROJECT_UUID --env staging --file .env.preview
```

`--project` overrides the project for that command without changing `.envault.json`. If the target is on a different installation, specify both values and log in there first:

```sh
envault login --api-url https://other-vault.example.com
envault pull --api-url https://other-vault.example.com --project PROJECT_UUID --file .env.preview
```

Project operations reject `--api-url` that conflicts with a saved binding unless `--project` is also explicit. This prevents silently sending an existing project's operations to another server.

### Permanently relink a directory

```sh
envault init --force
# Pick a different project
```

For scripts, supply the target explicitly:

```sh
envault init --project PROJECT_UUID --force
```

Without `--force`, replacing an existing binding requires confirmation in a terminal and is refused noninteractively. Relinking does not move or copy remote secrets. If a parent directory supplies the existing binding, `init` updates that nearest binding; it does not create a separate child binding. Plan directory boundaries accordingly, especially in a monorepo.

### Choose the environment for every command

Each project has exactly `dev`, `staging`, and `prod`. The default is always `dev`; using `--env prod` does not save a new default for future human commands.

```sh
envault pull --env dev --file .env.dev
envault pull --env staging --file .env.staging
envault pull --env prod --file .env.production
```

Names such as `.env.staging` do not select the remote environment. The `--env` flag does.

## Upload, download, and run

### First upload to an empty environment

After creating/linking the project and preparing your local file:

```sh
envault status
envault push --env dev --file .env
```

The CLI asks for your vault password, encrypts the complete file locally, and uploads ciphertext. A successful push records the remote revision as the base for that exact local file. Plaintext is limited to 1 MiB per snapshot.

Each project/environment stores one sequence of whole-file snapshots. `--file` chooses a local input or output path; it does not create multiple named remote files. Pushing `.env.shared` to `dev` and then pushing `.env.private` to `dev` replaces the current `dev` snapshot rather than merging variables or storing two files. Combine the intended contents locally before uploading.

### Daily editing when remote secrets already exist

```sh
envault pull --env dev --file .env
# Edit .env locally.
envault push --env dev --file .env
```

Pull decrypts locally and writes a protected regular file. It refuses to overwrite an existing file unless you use `--force`:

```sh
# Back up or reconcile existing local edits before running this.
envault pull --env dev --file .env --force
```

`--force` on pull authorizes local replacement. It does not change the remote environment. Symlink output is refused. Unix output files use mode 0600; Windows output uses an owner/System DACL. Push/pull preserve file bytes, including BOM, CRLF, multiline values, and trailing newlines.

### Use different local files for different environments

```sh
envault push --env dev --file .env.local
envault push --env staging --file .env.staging
envault push --env prod --file .env.production
```

For nonempty remote environments, pull into that same local file first and reconcile edits before pushing. A successful pull into `.env.download` does not establish the upload base for `.env.production`.

### Start a program without writing an environment file

```sh
envault run -- npm run dev
envault run --env staging -- python worker.py
envault run --env prod -- ./server
```

`run` downloads and decrypts the selected environment in memory, parses its dotenv contents, and starts the executable after `--`. It has no `--file` flag and always reads the remote current snapshot. Secret variables override inherited variables with the same name for that child process. The parent shell is unchanged.

Supported parsing includes comments, optional `export`, quoted/multiline values, and defined double-quote escapes. Duplicate variable names and malformed syntax fail. `$VAR`, `${VAR}`, backticks, and command substitution are not expanded. See the [exact dotenv grammar](../protocol/dotenv.md).

The CLI starts the executable directly. Shell operators such as `&&`, pipes, and redirects are not interpreted unless you explicitly launch a shell. The child keeps its normal input/output and exit status. Ctrl+C is forwarded to the process tree, with five seconds for graceful shutdown before remaining descendants are terminated. On POSIX, signal exits use `128 + signal`.

The child application can still log or persist secrets. Use normal application logging discipline even when `run` avoids writing a `.env` file.

## Conflicts, history, and restoration

### Resolve a stale local file safely

A normal push checks the last successful pull/push base for the exact installation, project, environment, and absolute local file path. If somebody else pushes, your older file must not overwrite their changes silently. If the remote already has a head but this file has no recorded base, normal push also refuses.

For a Bash/zsh example, first protect a backup of your edits, then refresh and merge:

```sh
umask 077
cp .env .env.local-backup
chmod 600 .env.local-backup
envault pull --env dev --file .env --force
# Merge the needed edits from .env.local-backup into the refreshed .env.
envault push --env dev --file .env
# Securely manage/remove the plaintext backup when reconciliation is finished.
```

Use equivalent protected file operations on Windows. The backup is sensitive and must stay out of Git. If another push happens during the merge, repeat reconciliation. EnVault does not automatically merge dotenv files.

When replacement is genuinely intended:

```sh
envault push --env dev --file .env --overwrite-remote
```

This bypasses the normal local-base safeguard. It still performs server-side concurrency and epoch checks, but it can replace changes you have not incorporated. Do not use it as the routine response to a conflict.

### Browse and download history

```sh
envault history --env staging
# Use the numeric nextCursor returned by the previous page:
envault history --env staging --before 42

# Download a historical snapshot without changing the remote head:
envault pull --env staging --version REVISION_UUID --file .env.previous
```

The history output contains `revisionId`, display `version`, epoch, author, and timestamp metadata. Use the **revision UUID**, not the display number, with `pull --version` and `restore`. You need the historical epoch key to decrypt an old snapshot. A newly granted member may have only current-epoch access.

### Restore an earlier snapshot as the new current version

```sh
envault restore REVISION_UUID --env staging
```

Restore decrypts the selected historical snapshot locally and uploads a newly encrypted version under the current environment key. It deliberately replaces the current remote contents, retains history, and requires write access. Review the selected revision and coordinate with teammates before running it. It does not update your local `.env`; pull/reconcile afterward to refresh the file and its upload base. Rotation also requires a fresh pull/reconciliation before continuing from an old local base.

## Team access and ownership

Project membership and decryption access are separate. A role change alone does not distribute environment keys.

| Role        | Practical capabilities                                                                                                                                                                   |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `developer` | Read granted environments; push/restore only where an explicit write grant exists.                                                                                                       |
| `admin`     | Manage invitations, developer removals, grants, machine credentials, and rotation; cryptographic operations also require the relevant environment write access/key. Cannot change roles. |
| `owner`     | Full project access and role management. A project must retain an owner; promoting another owner requires verified access confirmation.                                                  |

Owners can remove nonowners. Admins can remove developers, not owners or other admins. Member and credential listing is for owners/admins. Read access necessarily permits obtaining plaintext locally; it cannot prevent copying.

### Invite and grant access

Owner/admin, from the linked project:

```sh
envault members invite GITHUB_NUMERIC_ID
```

Use the recipient's numeric GitHub account ID, not username or EnVault user UUID. Send the returned invitation URL through your team's trusted channel. Invitations expire after seven days and are bound to that GitHub identity.

The teammate signs into the dashboard, creates their own vault/recovery kit if needed, logs into the CLI, and accepts the URL in the dashboard or the token with:

```sh
envault members accept INVITATION_TOKEN
```

Acceptance reports the project ID but does not link the local directory or grant environment keys. The teammate can then run `envault init`. Acceptance initially creates developer membership awaiting a verified grant.

The owner/admin lists members, obtains the teammate's EnVault `userId`, and independently verifies their identity fingerprint. The teammate can obtain their fingerprint and account ID from the dashboard quick reference. The CLI has no separate fingerprint-display command.

```sh
envault members list --limit 20
envault members grant USER_UUID --env dev --fingerprint 'VERIFIED_FINGERPRINT' --write
envault members grant USER_UUID --env staging --fingerprint 'VERIFIED_FINGERPRINT'
```

`--write` permits uploads/restores; omitting it gives read-only access and updates an existing grant to read-only. The grant wraps the current environment key for that teammate. Repeat per environment. Do not substitute a fingerprint copied solely from the same untrusted directory for independent identity verification. A changed saved pin fails until the identity has been investigated and the specific old local pin is deliberately managed.

### Remove a member and rotate affected keys

```sh
envault members remove USER_UUID
```

This CLI command removes project membership and grants; it has no per-environment removal flag. Affected environments require rotation before further writes. Use the dashboard or returned state to identify them, then perform each rotation with an authorized owner/admin:

```sh
envault rotate --env dev
# This prints recipient fingerprints and exits without rotating.
# Verify every displayed recipient independently, then:
envault rotate --env dev --verified-recipients
```

The first command asks for the vault password and returns a nonzero result on purpose until recipients are confirmed. Rotation generates a new environment key, re-encrypts the current snapshot, and wraps the new key for every active recipient. Regranted users get the current key, not automatically the old history.

Revocation and rotation cannot erase files, keys, or plaintext a former member already downloaded. If a real database password or provider API key was exposed, rotate that credential at its source and publish the replacement through EnVault too.

### Roles and ownership transfer

Only an owner can change another member's role:

```sh
envault members role USER_UUID admin
envault members role USER_UUID developer
```

For a new owner:

1. The current owner/admin grants the recipient write access to `dev`, `staging`, and `prod`, verifying the fingerprint each time.
2. The recipient runs the following command in their own authenticated CLI; it locally decrypts all three current keys:

   ```sh
   envault members confirm-ownership
   ```

3. The current owner promotes that recipient:

   ```sh
   envault members role USER_UUID owner
   ```

If grants or rotation changed after confirmation, repeat confirmation. An owner cannot remove themselves through role management, owners cannot be removed as members, and the last owner cannot be demoted. Keep at least one working owner identity and recovery kit.

## CI and machine credentials

Machine credentials are for read-only unattended consumption of one project's one environment. They contain a separate private key and an API token, so they do not need a human vault password or OS keychain in CI.

As an owner/admin with write access to the chosen environment:

```sh
envault credentials create production-deploy --env prod --days 30
```

The JSON bundle is printed to stdout only once. Store the complete JSON as a protected, masked CI secret named `ENVAULT_MACHINE`; do not paste it into logs, issue trackers, or repository files. Creation defaults to 30 days and accepts 1–365 days. Labels do not replace credential UUIDs for revocation.

If you must stage the output as a local file, choose a protected location outside the repository and restrictive OS permissions first. Do not redirect it to an ordinary tracked JSON file.

In the CI runner, have the CI system inject `ENVAULT_MACHINE`, install the verified CLI, and run:

```sh
envault run -- npm run build
```

The bundle selects its installation, project, and environment even though the human default is `dev`. No `login` or `init` is needed. Conflicting explicit `--api-url`, `--project`, or `--env` values are rejected. A conflicting local project binding can also fail, so use the correct project directory or a clean runner directory. The bundle itself is removed from the child's inherited environment; the actual secret variables are injected.

Machine credentials can also pull the current snapshot if a deployment genuinely needs a file. They cannot push, restore, list history, create projects, or administer access. `status` reports the configured machine scope without checking its token remotely; a successful `run`/`pull` is the actual consumption check.

Manage them as a human owner/admin:

```sh
envault credentials list --limit 20
envault credentials revoke CREDENTIAL_UUID
```

After revocation, rotate the affected environment using the verified-recipient workflow. Expired machine credentials also make environment writes require rotation. Remaining active bundles continue working after rotation because new keys are wrapped to their existing public keys. For renewal, create a replacement bundle, update the CI secret, verify it, then revoke the superseded credential and rotate as required. Do not run credential-administration commands with `ENVAULT_MACHINE` still set in your human terminal.

## Recovery and logout

### Recover a forgotten vault password

You still need access to the same GitHub-backed EnVault account and installation:

```sh
envault login
envault recover --kit /secure/path/envault-recovery-kit.json
```

The CLI checks the kit's schema, installation, and account, then asks for a new vault password of at least 12 characters. Recovery rewraps the same account vault key and preserves the identity key, so existing environment wraps remain usable. It does not reset GitHub access, create a new identity, or need to re-encrypt every secret. The kit remains a sensitive decryption credential after recovery. A kit for another hostname/installation is rejected; installation-origin changes require a reviewed recovery/migration procedure.

If both the vault password and recovery kit are lost, the application has no server-side vault reset that restores the encrypted identity. Losing GitHub access is a separate authentication-recovery problem.

### Log out or revoke a lost device

```sh
envault logout
```

Logout revokes/removes the selected installation's human CLI credential. It retains the remembered installation and local project binding, and does not delete any plaintext files previously pulled to disk. It does not log out every machine or other browser/CLI session.

For a lost laptop/session, use the dashboard's account CLI-session controls to revoke that session or all human CLI sessions. Machine credentials have the separate project credential controls described above.

## Complete command reference

All commands support `--help`. Capitalized values below are placeholders, not literals.

| Command                                                               | Use it for                                                                      | Vault unlock needed?                         |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------- | -------------------------------------------- |
| `envault login [--no-browser]`                                        | Approve a new human CLI session in the browser and remember its installation.   | No                                           |
| `envault logout`                                                      | Revoke/remove the selected human CLI credential.                                | No                                           |
| `envault status`                                                      | Show resolved installation, project, and authentication state.                  | No                                           |
| `envault init [--project PROJECT_UUID \| --name NAME] [--force]`      | Link, interactively select, create, or deliberately replace a local binding.    | Only creation                                |
| `envault projects [--limit N] [--before UUID_CURSOR]`                 | List accessible projects as JSON.                                               | No                                           |
| `envault push [--file PATH] [--overwrite-remote]`                     | Encrypt/upload a whole environment-file snapshot; default input `.env`.         | Yes                                          |
| `envault pull [--file PATH] [--version REVISION_UUID] [--force]`      | Decrypt/download current or historical snapshot; default output `.env`.         | Yes; machine current reads use their own key |
| `envault run -- EXECUTABLE [ARGS...]`                                 | Inject current remote secrets into a child without a `.env` file.               | Yes; machine runs use their own key          |
| `envault history [--before VERSION_CURSOR]`                           | List encrypted-version metadata; 50 entries per page.                           | No                                           |
| `envault restore REVISION_UUID`                                       | Publish historical contents as a new current version.                           | Yes                                          |
| `envault members list [--limit N] [--before UUID_CURSOR]`             | List membership, roles, and public keys/grants.                                 | No                                           |
| `envault members invite GITHUB_NUMERIC_ID`                            | Create a GitHub-identity-bound invitation.                                      | No                                           |
| `envault members accept INVITATION_TOKEN`                             | Accept an invitation after account/vault onboarding.                            | No                                           |
| `envault members grant USER_UUID --fingerprint FINGERPRINT [--write]` | Share the current environment key and set read/write permission.                | Yes                                          |
| `envault members remove USER_UUID`                                    | Remove a nonowner's project membership and freeze affected writes for rotation. | No                                           |
| `envault members role USER_UUID owner\|admin\|developer`              | Change another member's project role as an owner.                               | No                                           |
| `envault members confirm-ownership`                                   | Confirm local decryption access to all environments before promotion.           | Yes                                          |
| `envault credentials create LABEL [--days N]`                         | Export a one-time, environment-scoped machine bundle.                           | Yes                                          |
| `envault credentials list [--limit N] [--before UUID_CURSOR]`         | List project machine credential metadata.                                       | No                                           |
| `envault credentials revoke CREDENTIAL_UUID`                          | Revoke a machine credential and require environment rotation.                   | No                                           |
| `envault rotate [--verified-recipients]`                              | Inspect recipients, then rotate/re-encrypt the selected environment.            | Yes                                          |
| `envault recover --kit PATH`                                          | Recover the same account identity with its recovery kit and a new password.     | New password plus local recovery key         |
| `envault --version`                                                   | Print binary version; source builds may report `dev`.                           | No                                           |
| `envault help [COMMAND]`                                              | Show command help.                                                              | No                                           |
| `envault completion bash\|zsh\|fish\|powershell`                      | Generate shell completion script; see that shell's help for loading it.         | No                                           |

Completion shell subcommands also accept `--no-descriptions` to omit descriptive text from the generated completion script.

The unlock column describes human use unless a machine exception is stated. Metadata commands still require login and server-side permission. Run, push, pull, grants, creation, restore, rotation, and ownership confirmation prompt per invocation; login does not cache your vault password.

### Global flags and pagination

| Flag                                  | Default                                          | Meaning                                                                                         |
| ------------------------------------- | ------------------------------------------------ | ----------------------------------------------------------------------------------------------- |
| `--api-url ORIGIN`                    | Resolved from configuration, then hosted default | Select the installation for this command.                                                       |
| `--project PROJECT_UUID`              | Nearest `.envault.json`                          | Override the project for this command.                                                          |
| `--env dev\|staging\|prod`            | `dev` for humans                                 | Select the remote environment, not a local filename.                                            |
| `--password-stdin`                    | Off                                              | Explicitly read the vault/new password from one stdin line instead of a hidden terminal prompt. |
| `--help`, `-h`                        | Off                                              | Show the current command's help.                                                                |
| `--version`, `-v` on the root command | Off                                              | Print the CLI version; `pull --version UUID` instead selects a revision.                        |

`projects`, `members list`, and `credentials list` accept `--limit` (default 50, range 1–100) and `--before` using the UUID `nextCursor` from the preceding JSON page. `history` accepts a positive numeric version cursor and has no `--limit` flag. The project picker handles its own next-page selection. A null `nextCursor` means the end.

For `--password-stdin`, supply the password from a trusted secret source to stdin; do not put a real password in a command argument or shell-history example. Machine bundles are the intended unattended consumption mechanism. Interactive project selection and subsequent stdin password reading share the same input stream; scripts should use explicit `init` flags.

### Features provided by the dashboard rather than CLI commands

The current CLI has no project rename/delete, audit-log listing, standalone identity-fingerprint display, human-session list/revoke-all, persistent environment-default, named-server-profile, or per-environment member-removal command. Use supported dashboard/API controls where available; there is no project-delete operation documented here. Do not infer commands such as `envault switch`, `envault set`, `envault audit`, or `envault unlock` from other secret tools.

## Configuration, troubleshooting, and related documents

### What is stored locally?

| Item                                   | Location and content                                                                                                                                       |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Directory binding                      | Nearest `.envault.json`: `apiUrl` and `projectId`; no vault password or token.                                                                             |
| Remembered installation                | OS user-config directory under `envault/config.json`: only `apiUrl`.                                                                                       |
| Human API token                        | macOS Keychain, Windows Credential Manager, or Linux Secret Service under the EnVault service. No plaintext-token fallback.                                |
| Upload bases                           | OS user-config directory under `envault/heads/`: hashed filenames containing the last successful head, scoped to the exact remote and absolute local file. |
| Recipient fingerprints                 | OS user-config directory under `envault/fingerprints/`: verified public-key fingerprints.                                                                  |
| Downloaded environment files           | The paths you pass to `pull`; these contain plaintext.                                                                                                     |
| Recovery kit / exported machine bundle | Wherever you securely save/export them; both are sensitive decryption material.                                                                            |

Typical user-config roots are `~/Library/Application Support` on macOS, `$XDG_CONFIG_HOME` or `~/.config` on Linux, and `%AppData%` on Windows. The implementation uses the OS-provided user-config directory. User/project configuration writes are atomic and restrictive; malformed configuration is reported rather than silently ignored. Equivalent origin normalization preserves known legacy credentials and fingerprint pins.

### Troubleshooting

| Symptom                                                        | Next step                                                                                                      |
| -------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `unknown command "status"` or `init` still demands a UUID      | Check `--version`/`--help`; install/build the updated CLI.                                                     |
| Wrong project or server                                        | Run `status`, check the nearest parent binding, and use `init --force` for deliberate relinking.               |
| Cannot read OS keychain / Secret Service                       | Enable/unlock the platform keychain. Headless CI should use a machine bundle.                                  |
| Not logged in, expired/revoked session                         | Run the suggested `login` for that installation and approve the browser code.                                  |
| Verification URL belongs to another installation               | Have the operator check the application's configured origin; do not bypass the mismatch.                       |
| Local output already exists                                    | Back up/reconcile edits, then explicitly use `pull --force` if replacement is intended.                        |
| Unknown local base / stale conflict                            | Pull/reconcile using the same file path; see the conflict workflow.                                            |
| No secret versions                                             | Push an initial snapshot with an authorized human credential.                                                  |
| No key / access denied after invitation                        | Ask an owner/admin for a verified grant to the specific environment.                                           |
| No historical key                                              | Ask an owner to assess history access; a current grant alone does not supply old epochs.                       |
| Rotation required                                              | Complete verified rotation as an owner/admin; check removed or expired machine access.                         |
| Fingerprint changed                                            | Investigate and independently reverify the identity; do not blindly clear all pins.                            |
| Machine project/environment/installation mismatch              | Check the bundle, explicit flags, and nearest binding; do not repurpose a bundle for another scope.            |
| Vault password requires a terminal                             | Use an actual terminal, explicit `--password-stdin`, or a machine credential for unattended reads.             |
| `SETUP_REQUIRED`, `MIGRATION_REQUIRED`, `DATABASE_UNAVAILABLE` | These are application/operator setup issues, not a reason to replace your local secrets.                       |
| Push succeeded but base recording failed                       | The remote write may already exist. Protect the file and pull/reconcile before another push.                   |
| Project created but binding save failed                        | Keep the printed project ID, fix local permissions, then link that existing ID instead of creating duplicates. |

### Existing references

- [CLI README](../README.md): short overview and common commands.
- [Native releases, verification, and installation](releases.md): how to obtain and verify a binary.
- [Cryptographic protocol](../protocol/crypto.md): exact key hierarchy, algorithms, and authenticated contexts.
- [Dotenv grammar](../protocol/dotenv.md): accepted `run` syntax and literal/no-expansion rules.
- [OpenAPI contract](../protocol/openapi.json): versioned HTTP interface for implementers.

The private application repository additionally contains the end-to-end system architecture, architecture decisions, threat model, local setup/testing, operations, deployment, and verification guides. Those documents are linked from its README; they are not required or shipped as private dependencies of the public CLI guide.
