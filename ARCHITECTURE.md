# Architecture

Theseus is a Go rewrite of the Odysseus Python/FastAPI backend. The frontend (`static/`) is reused verbatim from Odysseus. The Go binary replaces `uvicorn app:app` and exposes the same REST + SSE API surface so the existing frontend works without modification.

---

## High-Level Overview

```
Browser / PWA
     │
     │  HTTP + SSE
     ▼
┌─────────────────────────────────────────────────────────┐
│                    Theseus Binary                        │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │ Auth         │  │ Request      │  │ Security      │  │
│  │ Middleware   │  │ Timeout MW   │  │ Headers MW    │  │
│  └──────┬───────┘  └──────┬───────┘  └───────┬───────┘  │
│         └─────────────────┴──────────────────┘          │
│                            │                             │
│                     ┌──────▼──────┐                      │
│                     │  HTTP Mux   │                      │
│                     └──────┬──────┘                      │
│                            │                             │
│   ┌────────────────────────┼────────────────────────┐   │
│   │                        │                        │   │
│   ▼                        ▼                        ▼   │
│ Chat/Agent            Sessions/Docs           Admin/    │
│ Handler               Memory/Skills           Settings  │
│   │                        │                            │
│   ▼                        ▼                            │
│ LLM Client            SQLite DB                         │
│ (streaming)           (modernc)                         │
│   │                                                      │
│   ▼                                                      │
│ Agent Loop ──► Tool Dispatcher ──► Built-in Tools        │
│                     │                                    │
│                     └──► MCP Client ──► External Servers │
│                                                          │
│  ChromaDB (optional)   SearXNG   ntfy   IMAP/SMTP        │
└─────────────────────────────────────────────────────────┘
```

---

## Package Structure

```
cmd/theseus/          Entry point — flag parsing, server init, HTTP listen
internal/
  auth/               Authentication, session tokens, TOTP, privilege checks
  db/                 SQLite schema, migrations, typed query helpers
  storage/            AES-256-GCM secret encryption, atomic JSON file I/O
  settings/           Settings/features JSON with TTL cache
  llm/                OpenAI-compatible streaming HTTP client
  agent/              Agent loop, fenced-block parser, SSE writer
  tools/              Built-in tool implementations (bash, python, files, docs, search)
  memory/             Memory manager, ChromaDB client, RAG, skills manager
  search/             Multi-provider web search (SearXNG, DDG, Brave, etc.)
  mcp/                MCP protocol client (stdio + SSE transports)
  research/           Deep research engine, HTML report generator
  calendar/           Calendar CRUD, CalDAV sync, iCal import/export
  email/              IMAP client, SMTP client
  cookbook/           Hardware detection, model catalog, tmux session manager
  tts/                TTS/STT provider abstraction
  webhook/            Outgoing webhook delivery, HMAC signing
  server/             HTTP route handlers, middleware, SSE helper
static/               Frontend HTML/CSS/JS (unchanged from Odysseus)
specs/                Spec documents (requirements, design, tasks, review)
```

---

## Core Components

### HTTP Server (`internal/server`)

The server uses Go's standard `net/http` with a `http.ServeMux` for routing. Three middleware layers wrap every request:

1. **SecurityHeadersMiddleware** — adds `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `X-XSS-Protection`, and a Content Security Policy header.
2. **RequestTimeoutMiddleware** — enforces a 45-second hard timeout on all non-streaming routes. Streaming routes (chat, shell, research, uploads) are exempt.
3. **AuthMiddleware** — validates session cookies and Bearer tokens. Supports an internal-tool loopback bypass for agent-to-API calls, and an optional localhost bypass for development.

Routes are registered in per-feature files (`session_routes.go`, `chat_routes.go`, etc.) and mounted during `server.New()`.

### Authentication (`internal/auth`)

Auth state is stored in `data/auth.json` (users + hashed passwords) and `data/sessions.json` (active session tokens). Both files are written atomically.

- **Passwords** are hashed with bcrypt (cost 12). Minimum length: 8 characters.
- **Session tokens** are 32-byte random hex strings. The token itself is never stored — only a bcrypt hash is persisted. Tokens expire after 7 days.
- **TOTP** secrets are encrypted at rest using AES-256-GCM before being written to `auth.json`.
- **Per-user privileges** control access to bash/python tools, image generation, memory management, and daily message limits.
- **API tokens** use the same bcrypt-hash-only storage pattern. The full token is returned once on creation and never again.

### Database (`internal/db`)

SQLite via `modernc.org/sqlite` (pure Go, no CGO). The schema covers 23 tables:

| Group | Tables |
|---|---|
| Chat | `sessions`, `chat_messages` |
| Documents | `documents`, `document_versions` |
| Memory | `memories` |
| Notes/Tasks | `notes`, `scheduled_tasks`, `task_runs` |
| Calendar | `calendar_cals`, `calendar_events` |
| Email | `email_accounts` |
| Gallery | `gallery_images`, `gallery_albums`, `editor_drafts` |
| Models | `model_endpoints`, `mcp_servers` |
| Compare | `comparisons` |
| Auth | `api_tokens`, `signatures` |
| Misc | `webhooks`, `user_tools`, `user_tool_data`, `crew_members` |

Migrations run at startup via `db.Migrate()`, which executes all `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS` statements idempotently. The connection pool is set to 1 open connection (SQLite serializes writes anyway).

### Secret Storage (`internal/storage`)

Sensitive values (email passwords, API keys, TOTP secrets, signatures) are encrypted with AES-256-GCM before being written to the database or config files.

- The encryption key is a 32-byte random value stored at `data/.app_key` (mode 0600).
- Encrypted values are prefixed with `enc:` followed by base64-encoded `nonce + ciphertext`.
- `Decrypt` passes through any value that doesn't start with `enc:` (legacy plaintext compatibility).
- `Encrypt` returns an error if the key has not been initialized — there is no silent plaintext fallback.

### LLM Client (`internal/llm`)

A reusable HTTP client for OpenAI-compatible `/v1/chat/completions` endpoints.

**Streaming:** Reads `data:` lines from the SSE response, unmarshals each JSON chunk, and emits `StreamChunk{Delta, FinishReason, ToolCalls}` values on a channel. The caller reads from the channel and forwards deltas to the browser via SSE.

**Dead-host cooldown:** After 2 consecutive connection failures to a host, that host is marked dead for 20 seconds. Subsequent requests fail immediately instead of waiting on a connect timeout. Any successful response resets the failure counter.

**Model discovery:** `DiscoverModels` fetches `/v1/models` and returns the list of model IDs. Results are cached per endpoint in the database.

### Agent Loop (`internal/agent`)

The agent loop implements the tool-use cycle:

```
1. Build messages (system prompt + history + tool schemas)
2. Stream LLM response
3. Parse fenced code blocks (```toolname\n...\n```) from response text
4. Also handle OpenAI function_calls from the stream
5. For each tool block: execute via Dispatcher, append result to messages
6. Repeat up to MAX_AGENT_ROUNDS (20)
7. If no tool blocks in a response: done
```

Tool results from native function calls use `role: "tool"`. Tool results from fenced-block calls use `role: "user"` (compatible with all models, not just function-calling ones).

The `SSEWriter` in `internal/server/sse.go` is a mutex-protected wrapper around `http.ResponseWriter` that serializes concurrent SSE writes (needed for the compare feature where two goroutines stream simultaneously).

### Tool Dispatcher (`internal/tools`)

Routes `ToolBlock{ToolType, Content}` to the correct implementation function. Privilege checks happen here before dispatch:

- `bash` / `python` require `can_use_bash: true`
- `generate_image` requires `can_generate_images: true`
- `manage_memory` requires `can_manage_memory: true`

Built-in tools:

| Tool | Implementation |
|---|---|
| `bash` | `exec.CommandContext` with PTY, uses caller's context for timeout |
| `python` | `exec.CommandContext("python3", "-c", code)` |
| `read_file` / `write_file` | Path-sandboxed to `DATA_DIR` via `safePath()` |
| `create_document` | Parses line-based or XML-tag format, language sniffing |
| `edit_document` | FIND/REPLACE block parsing, returns error if no match |
| `web_search` | Delegates to `internal/search` |
| All others | Stub returning "not yet implemented" or loopback HTTP call |

### Memory System (`internal/memory`)

Two-tier storage:

1. **SQL** (`memories` table) — always available, keyword search via `LIKE` queries.
2. **ChromaDB** (optional) — vector embeddings for semantic search. The `ChromaClient` uses ChromaDB's HTTP REST API (`/api/v1/collections`). Falls back to SQL keyword search if ChromaDB is unreachable.

The `Manager` is initialized once as a field on `Server` (not per-request) and shared across all handlers.

**Import deduplication** uses Jaccard similarity on tokenized text. Entries with similarity > 0.8 to an existing entry are skipped.

### Skills (`internal/memory/skills`)

Skills are SKILL.md files stored under `data/skills/<category>/<name>/SKILL.md`. The format is YAML frontmatter (name, description, category, status, confidence, author) followed by Markdown sections (When To Use, Procedure, Pitfalls, Verification).

The `SkillsManager` walks the skills directory on each `List` call. `RelevantSkills` scores skills by Jaccard similarity between the query and the skill's description + when_to_use + tags, then returns the top N above the confidence threshold.

### Search (`internal/search`)

A `Client` wraps multiple `Provider` implementations with a fallback chain and a TTL cache (5 minutes, keyed by `query|n`).

Providers: `SearXNGProvider`, `DuckDuckGoProvider` (HTML scrape), `BraveProvider`, `GooglePSEProvider`, `TavilyProvider`, `SerperProvider`.

`FetchContent` fetches a URL and runs it through `extractText`, a single-pass state machine that strips HTML tags and script/style blocks in O(n) time.

### MCP Client (`internal/mcp`)

Implements the Model Context Protocol client for both transports:

- **stdio** — spawns the server process, communicates via JSON-RPC over stdin/stdout. Sends `initialize` + `notifications/initialized` on connect, then `tools/list` to enumerate tools.
- **SSE** — sends JSON-RPC requests as HTTP POST to the server URL, reads the SSE response.

The `Manager` maintains a map of `ServerConn` objects. `ListTools` aggregates tools from all connected servers, filtering out per-server disabled tools. `CallTool` routes to the correct server by scanning tool names.

### Deep Research (`internal/research`)

Implements an iterative Think→Search→Extract→Synthesize loop:

```
1. Generate research plan (sub-questions, key topics, success criteria)
2. For each round (up to 5):
   a. Generate 2-3 search queries based on plan + current report
   b. Search each query, fetch and extract content from result URLs
   c. Synthesize new findings into the growing report
   d. Check if report is substantial enough to stop
3. Generate HTML report from final markdown
```

Progress events are streamed to the browser via SSE. The HTML report is self-contained with dark/light theme, table of contents, and a collapsible sources list.

### Calendar (`internal/calendar`)

Local storage in SQLite (`calendar_cals`, `calendar_events`). CalDAV sync uses `github.com/emersion/go-webdav/caldav`:

1. Discover calendars via `FindCurrentUserPrincipal` → `FindCalendarHomeSet` → `FindCalendars`
2. Query events in a ±90 day / +1 year window via `QueryCalendar`
3. Parse VEVENT components from `go-ical` objects
4. Upsert events by UID; delete events no longer present on the server

iCal import/export is handled by `ParseICS` and `ExportICS` in `internal/calendar/webdav.go`.

### Email (`internal/email`)

**IMAP** uses `github.com/emersion/go-imap/v2`. Key operations:
- `UIDSearch` for listing messages (returns UIDs, not sequence numbers)
- `Fetch` with `BodySection` for message content
- `Store` + `UIDExpunge` for deletion (expunges only the specified UIDs, not all deleted messages)
- `Move` for archiving to another folder

**SMTP** uses `net/smtp`. Port 465 uses implicit TLS (`tls.Dial`); port 587 uses STARTTLS (`smtp.SendMail`). MIME boundaries are randomly generated per message. Subject and From headers are RFC 2047 encoded if they contain non-ASCII characters.

Email passwords are encrypted at rest via `storage.Encrypt`.

### Cookbook (`internal/cookbook`)

**Hardware detection** reads GPU VRAM from `nvidia-smi` output, RAM from `/proc/meminfo` (Linux) or `sysctl hw.memsize` (macOS), and CPU count from `runtime.NumCPU()`.

**Model catalog** is embedded at compile time from `internal/cookbook/data/hf_models.json` (270+ models with VRAM/RAM requirements, format, backend, and HuggingFace repo ID). `RecommendModels` scores each model with `FitScore` and returns them sorted by score.

**Serving** uses tmux sessions. `StartDownload` and `StartServe` create named tmux sessions running `huggingface-cli download` or `vllm`/`llama-server`. Log output is tailed from a file in `$TMPDIR/theseus-tmux/`.

### Webhook Manager (`internal/webhook`)

`Fire(ctx, event, payload)` looks up active webhooks for the event, then spawns a goroutine per webhook using `context.Background()` with a 15-second timeout (independent of the triggering request's context). Each delivery:

1. Marshals the payload as JSON
2. Computes HMAC-SHA256 signature using the webhook's secret
3. POSTs to the webhook URL with `X-Theseus-Signature: sha256=<hex>`
4. Records the status code and any error in the database

`ValidateWebhookURL` requires `http` or `https` scheme and blocks loopback addresses to prevent SSRF.

---

## Data Flow: Chat Request

```
POST /api/chat
  │
  ├─ AuthMiddleware: validate session cookie → set current_user in context
  ├─ RequestTimeoutMiddleware: exempt (streaming path)
  │
  ▼
handleChat()
  ├─ Load session from DB
  ├─ Ownership check
  ├─ Persist user message to chat_messages
  ├─ Load message history from DB
  ├─ Detect tool intent (regex patterns) → escalate to agent if matched
  │
  ├─ [plain chat] llm.Stream() → forward deltas as SSE → persist assistant message
  │
  └─ [agent mode] agent.Run()
       ├─ Build messages with system prompt + tool schemas
       ├─ llm.Stream() → accumulate response
       ├─ ParseToolBlocks() → find fenced code blocks
       ├─ For each block: tools.Dispatcher.Execute()
       │    ├─ Privilege check
       │    └─ DoBash() / DoWebSearch() / DoCreateDocument() / etc.
       ├─ Append tool result to messages
       └─ Repeat up to 20 rounds
```

## Data Flow: Agent Tool → Internal API

Some agent tools (e.g. `manage_notes`, `manage_calendar`) make loopback HTTP calls back to Theseus's own API routes. These calls include an `X-Theseus-Internal` header with a shared secret token. The `AuthMiddleware` recognizes this header from loopback clients and grants access without a session cookie, attributing the request to the user who triggered the agent via `X-Theseus-Owner`.

---

## Concurrency Model

- Each HTTP request runs in its own goroutine (standard `net/http`).
- The agent loop runs in the request goroutine; tool calls use `exec.CommandContext` with the request context.
- Background pollers (email, CalDAV, task scheduler) run as long-lived goroutines started at boot.
- The settings cache uses `sync.RWMutex` for safe concurrent reads with infrequent writes.
- The `RateLimiter` uses `sync.Mutex` to protect its per-user count maps.
- The `SSEWriter` uses `sync.Mutex` to serialize concurrent writes to a single `http.ResponseWriter` (used in the compare feature).
- The auth `Manager` uses `sync.RWMutex` for all config and session map access.
- SQLite is configured with `SetMaxOpenConns(1)` — all writes are serialized by the driver.

---

## Frontend Compatibility

The frontend is the original Odysseus `static/` directory, served unchanged. The Go backend maintains full API compatibility:

- All route paths match the Python originals exactly.
- JSON response shapes match field-for-field.
- SSE event names and data formats are identical.
- Session cookie name (`session_token`) is unchanged.
- The SPA fallback serves `static/index.html` for all non-API, non-static paths.
- `sw.js` is served with `Service-Worker-Allowed: /` to enable the PWA service worker.

---

## Key Dependencies

| Package | Purpose |
|---|---|
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGO) |
| `golang.org/x/crypto/bcrypt` | Password hashing |
| `github.com/pquerna/otp` | TOTP (2FA) |
| `github.com/creack/pty` | PTY for shell streaming |
| `github.com/emersion/go-imap/v2` | IMAP client |
| `github.com/emersion/go-message` | MIME message parsing |
| `github.com/emersion/go-webdav` | CalDAV / CardDAV |
| `github.com/emersion/go-ical` | iCalendar parsing |
| `github.com/rwcarlsen/goexif` | EXIF metadata extraction |
| `github.com/google/uuid` | UUID generation |
| `github.com/mark3labs/mcp-go` | MCP protocol (reference) |

All dependencies are pinned in `go.sum`. The binary is statically linked and has no runtime dependencies beyond the OS.
