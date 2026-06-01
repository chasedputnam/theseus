# Security

This document describes the security controls implemented in Theseus, the threat model they address, and important considerations for operators deploying it.

---

## Threat Model

Theseus is designed for **trusted-network deployment** — a personal server, home lab, or private VPN. The primary threats it defends against are:

- **Unauthorized access** to the workspace by unauthenticated users
- **Privilege escalation** by authenticated non-admin users
- **Data exposure** from stolen backups or database files
- **Server-Side Request Forgery (SSRF)** via webhook or agent HTTP calls
- **Path traversal** via agent file tools
- **Prompt injection** from untrusted content processed by the agent
- **Credential exposure** in process listings or log files

It is **not** designed to defend against a fully compromised host OS, a malicious admin user, or a sophisticated attacker with direct filesystem access.

---

## Authentication

### Session Tokens

- Session tokens are 32-byte cryptographically random values generated with `crypto/rand`.
- The token itself is **never stored**. Only a bcrypt hash (cost 12) is persisted in `data/sessions.json`.
- Tokens are transmitted as HTTP-only cookies (`session_token`). They cannot be read by JavaScript.
- Tokens expire after 7 days. Expired tokens are pruned on load.
- Logging out immediately invalidates the token by removing it from the session store.
- Deleting a user immediately revokes all their active session tokens.

### Passwords

- Passwords are hashed with bcrypt (cost 12) before storage. Plaintext passwords are never written to disk or logs.
- Minimum password length: 8 characters.
- Password changes require the current password to be provided.

### Two-Factor Authentication (TOTP)

- TOTP is implemented per RFC 6238 using `github.com/pquerna/otp`.
- TOTP secrets are encrypted with AES-256-GCM before being written to `data/auth.json`. They are never stored in plaintext.
- When 2FA is enabled, a valid TOTP code is required on every login in addition to the password.

### API Tokens

- API tokens are 32-byte random hex strings generated with `crypto/rand`.
- Only a bcrypt hash and an 8-character display prefix are stored. The full token is returned once on creation and cannot be retrieved again.
- Tokens carry scopes (e.g. `chat`) that limit what they can do.
- Tokens can be revoked at any time from the Settings panel.

### Internal Tool Bypass

The agent loop makes loopback HTTP calls back to Theseus's own API (e.g. to create notes or calendar events). These calls use a shared secret token in the `X-Theseus-Internal` header. The bypass is only honored when:

1. The request originates from `127.0.0.1` or `::1` (loopback only).
2. The header value matches the server's internal token exactly.

This prevents external callers from impersonating the internal tool path.

---

## Authorization

### Role-Based Access

Every route is protected by one of three access levels:

| Level | Who | Examples |
|---|---|---|
| Public | Anyone | `/api/auth/login`, `/api/health` |
| Authenticated | Any logged-in user | Chat, documents, notes, calendar |
| Admin only | Admin users | Shell execution, model endpoints, MCP servers, user management, wipe, webhooks |

### Per-User Privileges

Non-admin users have a privilege map that controls access to specific capabilities:

| Privilege | Default | Controls |
|---|---|---|
| `can_use_bash` | `false` | Shell and Python tool execution in the agent |
| `can_use_agent` | `true` | Agent mode |
| `can_use_browser` | `true` | Browser tool (if implemented) |
| `can_use_documents` | `true` | Document editor |
| `can_use_research` | `true` | Deep research |
| `can_generate_images` | `true` | Image generation tool |
| `can_manage_memory` | `true` | Memory add/delete |
| `max_messages_per_day` | `0` (unlimited) | Daily message rate limit |
| `allowed_models` | `[]` (all) | Restrict to specific model IDs |

Admins always have all privileges. Privilege checks happen in the tool dispatcher before any tool executes.

### Ownership Checks

All user data (sessions, documents, notes, memories, gallery images, etc.) is tagged with an `owner` field. Route handlers verify that the requesting user owns the resource before allowing read or write access. Admins can access all resources.

---

## Encryption at Rest

### Application Encryption Key

A 32-byte random key is generated on first boot and stored at `data/.app_key` with mode `0600`. This key is used for all AES-256-GCM encryption in the application.

**If this file is lost, all encrypted data becomes unrecoverable.** Back it up alongside `data/app.db`.

### Encrypted Fields

The following values are encrypted with AES-256-GCM before being written to the database or config files:

| Field | Location |
|---|---|
| Email account passwords (IMAP + SMTP) | `email_accounts` table |
| Model endpoint API keys | `model_endpoints` table |
| User signatures (handwritten PNG/SVG) | `signatures` table |
| TOTP secrets | `data/auth.json` |
| Vault session token | `data/vault.json` |

**Encryption scheme:** AES-256-GCM with a 12-byte random nonce prepended to the ciphertext. The result is base64-encoded and prefixed with `enc:`. Values without the `enc:` prefix are treated as legacy plaintext and passed through unchanged (for migration compatibility).

### Database File

The SQLite database (`data/app.db`) is not encrypted at the file level. The threat model for field-level encryption is a **stolen backup** — an attacker who obtains the database file but not the `.app_key` file cannot decrypt the sensitive fields. An attacker with access to both files can decrypt everything.

For full database encryption, consider running Theseus on an encrypted filesystem (e.g. LUKS on Linux).

---

## Transport Security

Theseus serves plain HTTP. For any deployment accessible outside `localhost`:

- **Always put a TLS-terminating reverse proxy in front** (Caddy, nginx, Traefik).
- Without TLS, session cookies and API tokens travel in cleartext and can be intercepted.
- The browser will warn about "Password fields on an insecure page" without HTTPS.

See the [README](README.md#putting-theseus-behind-https) for reverse proxy configuration examples.

### Security Headers

Every HTTP response includes:

| Header | Value |
|---|---|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `SAMEORIGIN` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `X-XSS-Protection` | `1; mode=block` |
| `Content-Security-Policy` | `default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; ...` |

The CSP currently allows `'unsafe-inline'` and `'unsafe-eval'` for scripts because the frontend uses inline event handlers and dynamic evaluation. This is a known limitation inherited from the Odysseus frontend.

---

## Shell Execution

The `bash` and `python` agent tools execute arbitrary commands on the server. This is intentional — it's what makes the agent powerful — but it requires careful access control.

**Controls in place:**

- Shell tools require `can_use_bash: true` in the user's privilege map. This is `false` by default for non-admin users.
- The `/api/shell/exec` and `/api/shell/stream` routes are admin-only.
- Shell commands run as the OS user that started Theseus. Do not run Theseus as root.
- The `tmux` log session name is validated to contain only `[a-zA-Z0-9_-]` to prevent path traversal in log file access.

**What this does not protect against:**

- A malicious admin user (they have full shell access by design).
- A non-admin user who has been granted `can_use_bash: true`.
- Prompt injection attacks that trick the agent into running harmful commands (see below).

---

## File Access

The `read_file` and `write_file` agent tools are sandboxed to the `DATA_DIR` directory via `safePath()`:

1. Absolute paths are rejected.
2. The resolved path is checked with `filepath.Abs` to ensure it stays within `DATA_DIR`.
3. Path traversal sequences (`../`) are neutralized by `filepath.Clean` before the prefix check.

If `filepath.Abs` fails (e.g. invalid directory), the operation returns an error rather than silently allowing or denying access.

---

## Prompt Injection

When the agent processes external content (web search results, email bodies, document contents), that content may contain instructions intended to hijack the agent's behavior ("ignore previous instructions, do X instead").

**Controls in place:**

- `PromptInjectionWarning` prepends a warning message to external content, telling the model to treat it as untrusted data.
- The agent system prompt instructs the model to be skeptical of instructions embedded in tool results.

**Limitations:** Prompt injection is an unsolved problem in LLM security. These controls reduce the risk but do not eliminate it. Do not grant the agent access to sensitive operations (e.g. sending emails, running shell commands) if it will be processing untrusted external content without human review.

---

## SSRF Prevention

Webhook URLs are validated before delivery:

- Must use `http` or `https` scheme (no `file://`, `ftp://`, etc.).
- Loopback addresses (`localhost`, `127.0.0.1`, `::1`) are blocked.
- URL length is capped at 2048 characters.

**Limitation:** Private IP ranges (RFC 1918: `10.x.x.x`, `172.16.x.x`, `192.168.x.x`) are not currently blocked. An attacker with webhook creation access could use webhooks to probe internal network services. If you expose Theseus to untrusted users who can create webhooks, consider adding RFC 1918 blocking.

---

## Rate Limiting

A per-user daily message limit can be set via the `max_messages_per_day` privilege (0 = unlimited). The `RateLimiter` is protected by a `sync.Mutex` for safe concurrent access. Limits reset at midnight UTC.

This is a soft limit intended for multi-user deployments to prevent one user from consuming all LLM capacity. It is not a security control against denial-of-service attacks.

---

## Request Timeouts

A hard 45-second timeout is enforced on all non-streaming routes. Requests that exceed this limit receive a `504 Gateway Timeout` response. This prevents hung LLM calls or slow database queries from holding goroutines indefinitely.

Exempt routes (streaming, long-running operations):
- `/api/chat` — streaming LLM responses
- `/api/shell/stream` — PTY streaming
- `/api/research` — multi-minute research jobs
- `/api/model/download` — model downloads
- `/api/cookbook/setup` — remote server setup
- `/api/upload` — large file uploads
- `/api/image` — image generation proxies

---

## Sensitive Data Handling

### Credentials

- Email passwords, API keys, and TOTP secrets are encrypted before storage (see [Encryption at Rest](#encryption-at-rest)).
- The vault master password is passed to the `bw` CLI via **stdin**, not as a command-line argument, to prevent exposure in `ps aux` output.
- API keys are masked in API responses (shown as `"stored"` after initial creation).

### Logs

- Passwords and tokens are never logged.
- The admin password is printed to stdout on first boot only, not to any log file.
- Structured logging uses `log.Printf` with explicit format strings — no automatic serialization of request bodies or sensitive structs.

### Signatures

Handwritten signatures (PNG/SVG) are encrypted at rest because they are biometric data. They are stored in the `signatures` table with AES-256-GCM encryption.

---

## Multi-User Deployments

If you run Theseus for multiple users:

1. **Disable open signup** unless you intend to allow self-registration. Go to **Settings → Auth → Signup** and set it to disabled.
2. **Review each user's privileges** before granting access. `can_use_bash` in particular gives a user the ability to run arbitrary code on your server.
3. **Create separate API tokens per integration** and set the minimum required scopes.
4. **Do not make users admins** unless they need full access. Admin users can read all other users' data, run shell commands, and wipe the database.
5. **Enable 2FA** for all admin accounts.

---

## Known Limitations

| Limitation | Notes |
|---|---|
| No file-level database encryption | Use an encrypted filesystem for full protection |
| CSP allows `unsafe-inline` / `unsafe-eval` | Inherited from the Odysseus frontend; requires frontend refactoring to fix |
| Webhook SSRF allows RFC 1918 targets | Private IP ranges are not blocked |
| Prompt injection is not fully preventable | Mitigated but not eliminated |
| Single encryption key for all secrets | Compromise of `.app_key` decrypts all encrypted fields |
| No audit log | Admin actions are not logged to a tamper-evident audit trail |
| Session tokens not rotated on privilege change | A token issued before a privilege downgrade retains the old privileges until it expires |

---

## Reporting Security Issues

If you discover a security vulnerability in Theseus, please report it responsibly. Do not open a public GitHub issue for security vulnerabilities. Contact the maintainer directly with details of the issue and steps to reproduce.
