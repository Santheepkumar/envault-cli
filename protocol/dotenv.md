# Environment-file grammar v1

Synchronization preserves opaque file bytes. Process injection requires UTF-8 without NUL bytes and follows `parser-vectors.json`, shared by Go and TypeScript tests.

- Normalize CRLF to LF for parsing only. Ignore blank lines and full-line comments.
- Accept optional `export ` before `NAME=value`. Names match `[A-Za-z_][A-Za-z0-9_]*`. Reject duplicates.
- Trim whitespace around names and unquoted values. An unquoted `#` starts a comment only at the beginning of the value or after a space/tab.
- Single and double quotes allow literal newlines. Single-quoted values are literal. Double-quoted values recognize `\n`, `\r`, `\t`, `\\`, and `\"`; preserve other backslash sequences.
- After a closing quote allow only whitespace, a comment, or newline. Reject unterminated quotes.
- Never expand `$VAR`, `${VAR}`, backticks, or `$(commands)`. Never evaluate a shell.
- Injected variables override inherited variables by name; Windows compares names without case and rejects input names that collide after case folding. Do not pass `ENVAULT_MACHINE` to the child.

The CLI launches the requested executable directly after `--`. A user may explicitly choose a shell, in which case that shell's own behavior applies to its arguments.
