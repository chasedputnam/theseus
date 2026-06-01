# Code Review Feedback

## Summary

The Go port is architecturally sound and builds cleanly. The core packages (auth, db, llm, agent, memory, search, storage) are well-structured and tested. The main concerns are: (1) several real blocking bugs independent of LSP stale-cache noise — privilege map aliasing, race conditions, SSRF in webhook validation, MIME boundary injection, and path traversal in shell routes; (2) widespread silent error swallowing on DB writes across server routes; (3) missing test coverage for security-critical paths.

---

## Findings

### internal/auth/manager.go

- [x] [BLOCKING] `DefaultPrivileges` map assigned by reference — all non-admin users share the same underlying map
  - Why: `privs := DefaultPrivileges` copies the map header, not the contents. Mutating one user's privileges mutates all.
  - Fix: `privs := make(map[string]any); for k,v := range DefaultPrivileges { privs[k] = v }`
  - References: Requirement 2

- [x] [SUGGESTION] `TOTPSecret` stored as plaintext in auth.json
  - Why: The `storage.Encrypt` facility exists; TOTP secrets should be encrypted at rest.
  - Fix: Encrypt on write, decrypt on read using `storage.Encrypt`/`storage.Decrypt`

- [x] [SUGGESTION] No password minimum-length validation in `createUserLocked`
  - Fix: Reject passwords shorter than 8 characters

- [x] [NIT] `migrateLegacy` is a no-op stub — remove or implement

### internal/auth/middleware.go

- [x] [BLOCKING] IPv6 address extraction via `strings.Split(r.RemoteAddr, ":")[0]` is wrong
  - Why: `[::1]:port` splits to `[` not `::1`, breaking localhost bypass for IPv6
  - Fix: Use `net.SplitHostPort(r.RemoteAddr)` instead

- [x] [SUGGESTION] `internalToolToken` is global mutable state
  - Fix: Inject as a parameter to `Middleware` rather than relying on `SetInternalToken`

### internal/storage/secrets.go

- [x] [BLOCKING] `loadOrCreateKey` silently overwrites a malformed key file
  - Why: If the file exists but `len(data) != 32`, a new key is generated and written, destroying the ability to decrypt existing data
  - Fix: Return an error if the file exists but has wrong length

- [x] [BLOCKING] `Encrypt` silently returns plaintext when `appKey == nil`
  - Why: Callers have no way to know encryption was skipped; silent security degradation
  - Fix: Return `("", fmt.Errorf("encryption key not initialized"))` when `appKey == nil`

### internal/db/db.go

- [x] [SUGGESTION] `min` function shadows Go 1.21+ builtin
  - Fix: Remove the local `min` definition; use the builtin

- [x] [SUGGESTION] No connection pool limits set
  - Fix: Call `db.SetMaxOpenConns(1)` for SQLite (it serializes writes anyway) and `db.SetMaxIdleConns(1)`

### internal/db/sessions.go

- [x] [BLOCKING] SQL string missing space before WHERE clause in `UpdateSession`
  - Why: `updated_at = ?WHERE id=?` produces invalid SQL at runtime
  - Fix: Add space/newline before `WHERE` in the UPDATE statement

### internal/agent/loop.go

- [x] [SUGGESTION] Tool results injected as `role: "user"` instead of `role: "tool"`
  - Why: Models using native function calling expect `role: "tool"` with `tool_call_id`
  - Fix: Use `role: "tool"` for function-call results; keep `role: "user"` for fenced-block results

### internal/agent/parser.go

- [x] [SUGGESTION] `toolTags` map has no validation against dispatcher — typos silently drop tool calls
  - Fix: Add a test that every tag in `toolTags` has a registered handler

### internal/tools/bash.go

- [x] [SUGGESTION] Timeout returns `nil` error — caller treats it as success
  - Why: `loop.go` only checks `err != nil`; a timed-out command is silently treated as success
  - Fix: Return a non-nil error on timeout, or document the contract explicitly

- [x] [SUGGESTION] Inner `context.WithTimeout` duplicates the outer `ToolTimeout` from `loop.go`
  - Fix: Use the incoming `ctx` directly; remove the inner timeout

### internal/tools/files.go

- [x] [BLOCKING] `filepath.Abs` errors silently ignored in `safePath`
  - Why: If `dataDir` is invalid, `absData` is empty and the prefix check incorrectly allows/denies paths
  - Fix: Return error if `filepath.Abs` fails

- [x] [SUGGESTION] Fallback from JSON parse failure to treating string as path is a footgun
  - Why: A malformed JSON object like `{"path": "/etc/passwd"` (missing brace) is treated as a literal path
  - Fix: Remove the fallback; require valid JSON

- [x] [NIT] `result[:maxReadChars]` slices bytes not runes — can split multi-byte UTF-8 characters
  - Fix: Use `[]rune` conversion for truncation

### internal/tools/documents.go

- [x] [SUGGESTION] `store.AddDocumentVersion` errors silently discarded in 3 places
  - Fix: Check and return errors from version writes

- [x] [SUGGESTION] "No matches found" returns success — misleads the LLM
  - Fix: Return a non-nil error or a clearly distinct status string

- [x] [NIT] `regexp.MustCompile` called inside `sniffLanguage` — recompiles on every call
  - Fix: Move to package-level `var`s

### internal/memory/manager.go

- [x] [SUGGESTION] `ListMemories` error silently ignored in `Import`
  - Fix: Return error if DB call fails

- [x] [SUGGESTION] ChromaDB upsert errors silently discarded
  - Fix: At minimum `log.Printf` the error

### internal/memory/skills.go

- [x] [BLOCKING] `Delete` with empty `category` or `name` could delete entire skills directory
  - Why: `filepath.Join(sm.skillsDir, "", "")` resolves to `sm.skillsDir`; `os.RemoveAll` on that deletes everything
  - Fix: Guard: `if category == "" || name == "" { return fmt.Errorf("category and name required") }`

- [x] [NIT] `os.MkdirAll` error ignored in `NewSkillsManager`
  - Fix: Return error from constructor or log it

### internal/search/client.go

- [x] [BLOCKING] `http.NewRequestWithContext` errors ignored with `req, _ :=` in 6 providers
  - Why: If URL is malformed, `req` is nil and `.Do(req)` panics
  - Fix: Check error and return it

- [x] [SUGGESTION] `extractText` is O(n²) for large pages
  - Fix: Use `golang.org/x/net/html` tokenizer or a compiled regex

### internal/server/security.go

- [x] [BLOCKING] `RateLimiter` has no mutex — data race on concurrent requests
  - Why: `Check` and `Increment` read/write shared maps from concurrent HTTP handlers
  - Fix: Add `sync.Mutex` to `RateLimiter`

- [x] [SUGGESTION] `RequestTimeoutMiddleware` panic recovery is racy
  - Why: `panicVal` written in goroutine, read in select without synchronization
  - Fix: Use a channel to communicate the panic value

### internal/server/compare_routes.go

- [x] [BLOCKING] Two goroutines call `sendEvent` concurrently — data race on ResponseWriter
  - Why: `streamModel` goroutines both write to `w` without synchronization
  - Fix: Protect `sendEvent` with a mutex

### internal/server/memory_routes.go

- [x] [SUGGESTION] `memoryManager()` creates new Manager per request with hardcoded disabled ChromaDB
  - Fix: Make `*memory.Manager` a field on `Server`, initialized once from config

- [x] [SUGGESTION] `s.db.AddMessage` and `s.db.ListMessages` errors ignored in `handleChat`
  - Fix: Check and return/log errors

### internal/server/shell_routes.go

- [x] [BLOCKING] `sessionName` from URL path used in file path without sanitization
  - Why: A `sessionName` containing `../` could read arbitrary files from temp directory
  - Fix: Validate `sessionName` contains only `[a-zA-Z0-9_-]`

### internal/server/vault_routes.go

- [x] [BLOCKING] Master password passed as CLI argument — visible in `ps aux`
  - Why: `exec.Command(bw, "unlock", "--raw", req.Password)` exposes password in process list
  - Fix: Pass via stdin: `cmd.Stdin = strings.NewReader(req.Password + "\n")`; use `bw unlock` without `--raw` or use `--passwordenv`

### internal/server/webhook_routes.go

- [x] [BLOCKING] `crypto/rand.Read` error ignored in token generation
  - Why: A failure produces a zero-byte token
  - Fix: Check error and return 500

- [x] [BLOCKING] `bcrypt.GenerateFromPassword` error ignored
  - Why: A bcrypt failure stores an empty hash, making the token permanently invalid
  - Fix: Check error and return 500

- [x] [NIT] `if true { // token cache invalidated }` — dead placeholder code
  - Fix: Remove

### internal/webhook/manager.go

- [x] [BLOCKING] Webhook delivery uses caller's context — cancelled when HTTP request completes
  - Why: `go m.deliver(ctx, hook, body)` — if the triggering request finishes, delivery is cancelled
  - Fix: Use `context.Background()` with a timeout for delivery goroutines

- [x] [BLOCKING] `ValidateWebhookURL` does not block SSRF
  - Why: Accepts `file://`, `ftp://`, private IPs — attacker can probe internal services
  - Fix: Parse URL, require `https` scheme, block RFC-1918 and loopback destinations

### internal/email/imap.go

- [x] [SUGGESTION] `DialStartTLS` passes nil TLS config — server certificate not verified
  - Fix: Pass `&imapclient.Options{TLSConfig: &tls.Config{ServerName: cfg.IMAPHost}}`

- [x] [SUGGESTION] `Expunge` expunges ALL deleted messages, not just the ones this call marked
  - Fix: Use `UIDExpunge` with the specific UIDs if the library supports it

### internal/email/smtp.go

- [x] [BLOCKING] Hardcoded MIME boundary `"theseus_boundary_12345"`
  - Why: If the email body contains this string, MIME structure breaks
  - Fix: Generate a random boundary per message

- [x] [SUGGESTION] `allTo := append(append(req.To, req.CC...), req.BCC...)` mutates caller's slice
  - Fix: `allTo := append([]string(nil), req.To...); allTo = append(allTo, req.CC...); allTo = append(allTo, req.BCC...)`

- [x] [SUGGESTION] Non-ASCII subject/from/to not RFC 2047 encoded
  - Fix: Use `mime.QEncoding.Encode("utf-8", value)` for header values

### Cross-cutting

- [x] [SUGGESTION] DB write errors silently ignored in ~15 places across server routes
  - Fix: Establish a pattern: always check DB write errors; log at ERROR level and return 500

- [x] [SUGGESTION] `sendEvent` SSE helper copy-pasted across 4 files
  - Fix: Extract to `internal/server/sse.go` using the existing `agent.SSEWriter` pattern

- [x] [SUGGESTION] No tests for security-critical paths: token generation, TOTP, shell privilege check, path traversal in `safePath`
  - Fix: Add targeted tests for each

---

## Positive observations

- Clean package separation — each feature is isolated with clear interfaces
- Dead-host cooldown in the LLM client is a good reliability pattern
- Atomic JSON writes throughout prevent partial-write corruption
- `safePath` in `tools/files.go` correctly prevents directory traversal (modulo the `filepath.Abs` error handling gap)
- HMAC-SHA256 webhook signing is correctly implemented
- AES-256-GCM with random nonce is the right choice for secret storage
- The agent loop's fenced-block parser correctly handles the `shell`/`sh`/`py` non-tool tags
- `saveSessionsLocked` vs `saveSessions` distinction correctly avoids the deadlock that was caught and fixed during development
