# EnVault cryptographic protocol v1

All strings use UTF-8 without Unicode normalization. Binary fields use padded standard RFC 4648 base64, never base64url. UUIDs are lowercase canonical strings. Envelope `v` must equal 1. Reject unsupported versions, unknown KDF parameters, invalid lengths, malformed encodings and failed authentication.

## Symmetric envelope

AES-256-GCM, 32-byte key, fresh CSPRNG 12-byte nonce per encryption, 128-bit tag appended to ciphertext. Maximum plaintext is 1,048,576 bytes. The serialized envelope is `{v,nonce,ciphertext}`. Never use an environment key for a different environment or epoch. Key envelopes contain exactly 32 plaintext bytes (48 ciphertext bytes).

Authenticated additional data is UTF-8 of the following exact strings:

| Purpose | Context |
| --- | --- |
| Secret version | `envault:v1:secret:<project UUID>:<dev|staging|prod>:<epoch decimal>:<revision UUID>` |
| Environment key wrap | `envault:v1:key:<project UUID>:<environment>:<epoch>:recipient:<recipient ID>` |
| Password account wrap | `envault:v1:account:<user UUID>:password` |
| Recovery account wrap | `envault:v1:account:<user UUID>:recovery` |
| Private identity key | `envault:v1:account:<user UUID>:identity` |

Clients construct contexts from the requested identifiers and reject inconsistent response identifiers before decryption. The server must not be allowed to relabel another environment's ciphertext and wrap as the requested environment. The server assigns the display version number; the client chooses the authenticated revision UUID before encryption.

## Account hierarchy

Generate independent random 32-byte account vault, X25519 private, and recovery keys. Derive a password key using Argon2id v19, 16-byte CSPRNG salt, memory 65,536 KiB, time 3, parallelism 4, output 32 bytes. Validate these exact parameters before starting work. Wrap the account vault key separately with the password key and recovery key. Encrypt the X25519 private key with the account vault key. The recovery kit contains the account ID, application origin, protocol version and recovery key; it is an offline decryption credential.

Recovery retains the identity key and recovery wrapper, changes the password salt, and rewraps the same account vault key. Updates use account-revision compare-and-swap. Account login alone cannot recover encrypted data. Retain access to the GitHub identity separately.

## Recipient wrapping

Generate a fresh random ephemeral X25519 key pair for each recipient wrapping operation. Compute the X25519 shared secret against the recipient public key; reject invalid/low-order public keys and all-zero shared secrets. Derive a wrapping key with HKDF-SHA256: input key material = shared secret, salt = SHA256(raw 32-byte ephemeral public key), info = the key-wrap context above, output length = 32 bytes. Encrypt the environment key with AES-256-GCM using the same context as additional data. Add `ephemeralPublicKey` to the envelope. Discard ephemeral private and derived keys after use.

User recipients use their account UUID; machine recipients use their credential UUID. Fingerprints are SHA256(raw public key), lowercase hexadecimal grouped in four-character blocks with spaces. Confirm through an independent trusted channel and pin before sharing. Changed pins fail closed.

## Trust and history

TLS authenticates the application endpoint. Fingerprints authenticate recipient identities out of band. The protocol protects ciphertext confidentiality and authenticated context; it does not provide a public transparency log, independent sender signatures, or a proof that a malicious service returned the newest version. The service can withhold or replay data. A malicious delivered browser bundle or compromised device can capture unlocked material. Memory clearing is best effort in garbage-collected runtimes.

Access removal immediately removes the recipient's server-side wraps and grants and freezes writes until key rotation. Regrant supplies only a current-epoch wrap. Other authorized recipients keep their historical wraps. Current secrets are re-encrypted on rotation; past downloaded secrets cannot be revoked. Rotate external API/database credentials when exposure matters.

`vectors.json` contains intentionally public test keys, passwords, recovery material, browser ciphertext, and Go ciphertext. Never use these values in production.
