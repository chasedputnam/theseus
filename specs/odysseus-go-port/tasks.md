# Implementation Tasks

- [ ] 1. Project scaffold & build system
  - Initialize Go module `github.com/chase.putnam/theseus` with `go mod init`
  - Create `cmd/theseus/main.go` entry point
  - Create directory structure: `internal/{server,auth,db,llm,agent,tools,chat,research,memory,email,calendar,notes,gallery,cookbook,search,settings,storage,webhook,tts,mcp}`
  - Add `go.sum`-pinned dependencies: `chi/v5`, `modernc.org/sqlite`, `golang.org/x/crypto` (bcrypt), `creack/pty`, `emersion/go-imap/v2`, `emersion/go-webdav`, `mark3labs/mcp-go`
  - Add `//go:embed static/*` directive and serve static files
  - Wire `--port`, `--data-dir`, `--static-dir` CLI flags
  - References: Requirement 1

  - [ ] 1.1 Initialize Go module and directory layout
  - [ ] 1.2 Add all third-party dependencies with pinned versions
  - [ ] 1.3 Implement static file serving with SPA fallback and embed support
  - [ ] 1.4 Implement `--port`, `--data-dir`, `--static-dir` flags and env var overrides
  - [ ] 1.5 Write smoke test: binary starts, serves `/api/health`, returns 200

- [ ] 2. Secret storage & atomic I/O
  - Implement `internal/storage/secrets.go`: AES-256-GCM encrypt/decrypt, key at `data/.app_key` (mode 0600)
  - Implement `internal/storage/atomic.go`: atomic JSON write (write to `.tmp`, rename)
  - Unit test: encrypt→decrypt round-trip; legacy plaintext passthrough
  - References: Requirement 27

  - [ ] 2.1 Implement AES-256-GCM encrypt/decrypt with `enc:` prefix
  - [ ] 2.2 Implement key generation and loading from `data/.app_key`
  - [ ] 2.3 Implement atomic JSON file write
  - [ ] 2.4 Unit tests for encrypt/decrypt and atomic write

- [ ] 3. Database layer & migrations
  - Implement `internal/db/db.go`: open SQLite, run migrations
  - Implement all table `CREATE TABLE IF NOT EXISTS` statements matching Python schema
  - Implement additive `ALTER TABLE ... ADD COLUMN` migrations for all columns added post-initial schema
  - Implement `internal/db/models.go`: Go structs for all 20+ tables
  - Implement typed query helpers for each table (insert, get by ID, list by owner, update, delete)
  - Implement startup migration for encrypting legacy plaintext secrets in `email_accounts`, `model_endpoints`, `signatures`
  - Unit test: migrations run twice without error; all tables present
  - References: Requirements 28, 27

  - [ ] 3.1 Implement base schema (sessions, chat_messages, documents, document_versions)
  - [ ] 3.2 Implement gallery, email, model_endpoints, mcp_servers tables
  - [ ] 3.3 Implement comparisons, signatures, api_tokens, webhooks tables
  - [ ] 3.4 Implement user_tools, crew_members, scheduled_tasks, task_runs tables
  - [ ] 3.5 Implement editor_drafts, memories, notes, calendar_cals, calendar_events tables
  - [ ] 3.6 Implement all additive column migrations (idempotent)
  - [ ] 3.7 Implement secret encryption migration on startup
  - [ ] 3.8 Unit tests: migration idempotency, all tables present, query helpers

- [ ] 4. Authentication & authorization
  - Implement `internal/auth/manager.go`: load/save `data/auth.json`, bcrypt hash/verify, session token CRUD, TOTP
  - Implement `internal/auth/middleware.go`: cookie + Bearer token auth, exempt paths, internal-tool loopback bypass, localhost bypass
  - Implement `internal/auth/privileges.go`: per-user privilege checks
  - Implement API token cache (prefix map, invalidation on create/revoke)
  - Implement auth routes: `/api/auth/login`, `/api/auth/logout`, `/api/auth/signup`, `/api/auth/setup`, `/api/auth/status`, `/api/auth/features`, `/api/auth/settings`
  - Implement admin user management routes: create, delete, list, update privileges
  - Unit tests: login/logout, token validation, privilege checks, TOTP, admin-only enforcement
  - References: Requirement 2

  - [ ] 4.1 Implement AuthManager: load/save auth.json, bcrypt, session tokens
  - [ ] 4.2 Implement TOTP (pyotp equivalent using `pquerna/otp`)
  - [ ] 4.3 Implement AuthMiddleware with all bypass rules
  - [ ] 4.4 Implement API token cache with invalidation
  - [ ] 4.5 Implement auth HTTP routes (login, logout, signup, setup, status)
  - [ ] 4.6 Implement admin user management routes
  - [ ] 4.7 Unit and integration tests for all auth flows

- [ ] 5. Settings & feature flags
  - Implement `internal/settings/settings.go`: load/save `data/settings.json` and `data/features.json` with 2s TTL cache
  - Implement all default values matching `DEFAULT_SETTINGS` and `DEFAULT_FEATURES` from Python
  - Implement settings routes: GET/POST `/api/settings`, GET/POST `/api/features`
  - Unit tests: TTL cache, defaults merge, atomic save
  - References: Requirement 26

  - [ ] 5.1 Implement settings load/save with TTL cache and defaults
  - [ ] 5.2 Implement features load/save with TTL cache and defaults
  - [ ] 5.3 Implement settings and features HTTP routes
  - [ ] 5.4 Unit tests for cache behavior and defaults

- [ ] 6. LLM client
  - Implement `internal/llm/client.go`: streaming HTTP client for OpenAI-compatible `/v1/chat/completions`
  - Implement SSE response parsing: `data:` lines → `StreamChunk{Delta, FinishReason, ToolCalls}`
  - Implement dead-host cooldown: 2 consecutive failures → 20s cooldown; success resets counter
  - Implement non-streaming `Call()` for utility/summarization use
  - Implement model discovery: GET `/v1/models` with caching
  - Unit tests: SSE parsing, dead-host logic, retry behavior
  - References: Requirement 3, 17

  - [ ] 6.1 Implement streaming LLM client with SSE parsing
  - [ ] 6.2 Implement dead-host cooldown and failure tracking
  - [ ] 6.3 Implement non-streaming Call() for utility use
  - [ ] 6.4 Implement model discovery via /v1/models
  - [ ] 6.5 Unit tests: SSE parsing, dead-host, retry

- [ ] 7. Session management
  - Implement `internal/db` session CRUD: create, get, list (by owner, archived, folder), update, delete, archive
  - Implement session routes: GET/POST/PUT/DELETE `/api/sessions`, `/api/sessions/{id}`, `/api/sessions/{id}/archive`, `/api/sessions/{id}/rename`, `/api/sessions/{id}/folder`
  - Implement message persistence: insert `chat_messages`, update `last_message_at`, `message_count`, token counts
  - References: Requirement 3

  - [ ] 7.1 Implement session CRUD in DB layer
  - [ ] 7.2 Implement session HTTP routes
  - [ ] 7.3 Implement message persistence with token tracking
  - [ ] 7.4 Integration tests for session lifecycle

- [ ] 8. Chat handler (streaming)
  - Implement `internal/chat/handler.go`: build context (system prompt + history + RAG), call LLM, stream SSE
  - Implement tool-intent pattern detection (escalate plain chat to agent loop)
  - Implement partial-response save on client disconnect
  - Implement privilege enforcement (`can_use_agent`, `max_messages_per_day`)
  - Implement chat routes: POST `/api/chat` (SSE stream), POST `/api/chat_stream`
  - Integration tests: mock LLM → assert SSE events, partial save on disconnect
  - References: Requirement 3

  - [ ] 8.1 Implement context builder (system prompt, history, RAG injection)
  - [ ] 8.2 Implement SSE streaming response handler
  - [ ] 8.3 Implement tool-intent escalation to agent loop
  - [ ] 8.4 Implement partial-response save on disconnect
  - [ ] 8.5 Implement daily message limit enforcement
  - [ ] 8.6 Integration tests for chat streaming

- [ ] 9. Agent loop & tool dispatch
  - Implement `internal/agent/loop.go`: multi-round loop, fenced-block parsing, function-call dispatch, SSE output
  - Implement `internal/agent/parser.go`: regex-based fenced code block parser (port of `tool_parsing.py`)
  - Implement `internal/tools/dispatcher.go`: route `ToolBlock` to correct `do_*` function
  - Implement agent system prompt injection (preamble + rules + tool schemas)
  - Implement per-user tool blocking via privileges
  - Unit tests: parser (all tool tags), loop termination at MAX_ROUNDS, tool blocking
  - References: Requirements 4, 5

  - [ ] 9.1 Implement fenced code block parser
  - [ ] 9.2 Implement agent loop with SSE streaming
  - [ ] 9.3 Implement tool dispatcher routing
  - [ ] 9.4 Implement agent system prompt with tool schemas
  - [ ] 9.5 Implement per-user tool privilege blocking
  - [ ] 9.6 Unit tests: parser, loop, blocking

- [ ] 10. Built-in tools — shell & Python
  - Implement `internal/tools/bash.go`: exec with PTY, 60s timeout, 10K char output truncation
  - Implement `internal/tools/python.go`: exec Python interpreter, 30s timeout
  - Implement `internal/tools/files.go`: read_file, write_file relative to data dir
  - Unit tests: timeout enforcement, output truncation, path sandboxing
  - References: Requirement 5

  - [ ] 10.1 Implement bash tool with PTY and timeout
  - [ ] 10.2 Implement python tool with timeout
  - [ ] 10.3 Implement read_file/write_file with path sandboxing
  - [ ] 10.4 Unit tests for all three tools

- [ ] 11. Built-in tools — documents
  - Implement `internal/tools/documents.go`: create_document, update_document, edit_document (FIND/REPLACE), suggest_document, manage_documents
  - Port language sniffing logic from `_sniff_doc_language`
  - Port email-document coercion logic
  - Unit tests: create, edit (FIND/REPLACE), version increment, language detection
  - References: Requirements 5, 8

  - [ ] 11.1 Implement create_document with language sniffing
  - [ ] 11.2 Implement edit_document with FIND/REPLACE block parsing
  - [ ] 11.3 Implement update_document (full replace)
  - [ ] 11.4 Implement manage_documents (list, archive, delete)
  - [ ] 11.5 Unit tests for all document tools

- [ ] 12. Document routes
  - Implement document HTTP routes: GET/POST/PUT/DELETE `/api/documents`, `/api/documents/{id}`, `/api/documents/{id}/versions`, `/api/documents/{id}/export`
  - Implement document search
  - Implement tidy verdict (keep/junk) endpoint
  - References: Requirement 8

  - [ ] 12.1 Implement document CRUD routes
  - [ ] 12.2 Implement document version history route
  - [ ] 12.3 Implement document export route
  - [ ] 12.4 Implement document search route
  - [ ] 12.5 Integration tests for document routes

- [ ] 13. Memory system
  - Implement `internal/memory/manager.go`: SQL-backed memory CRUD with keyword search
  - Implement `internal/memory/chroma.go`: ChromaDB HTTP client (collections, upsert, query, delete)
  - Implement graceful fallback when ChromaDB is unreachable
  - Implement memory extraction from chat history (LLM-based + regex fallback)
  - Implement memory injection into chat context
  - Implement memory routes: GET/POST/DELETE `/api/memory`, `/api/memory/search`, `/api/memory/import`
  - Unit tests: keyword search, ChromaDB fallback, dedup on import
  - References: Requirement 9

  - [ ] 13.1 Implement SQL memory CRUD and keyword search
  - [ ] 13.2 Implement ChromaDB HTTP client
  - [ ] 13.3 Implement ChromaDB fallback logic
  - [ ] 13.4 Implement memory extraction from chat
  - [ ] 13.5 Implement memory injection into context
  - [ ] 13.6 Implement memory HTTP routes
  - [ ] 13.7 Unit tests for memory system

- [ ] 14. Skills system
  - Implement `internal/memory/skills.go`: SKILL.md file CRUD under `data/skills/<category>/<name>/`
  - Implement skill injection into agent prompt (relevance scoring, max_injected limit, confidence gate)
  - Implement auto-save skill draft from agent completion
  - Implement skills routes: GET/POST/PUT/DELETE `/api/skills`, `/api/skills/{id}/publish`
  - Unit tests: SKILL.md parse/write, injection scoring, confidence gate
  - References: Requirement 10

  - [ ] 14.1 Implement SKILL.md file CRUD
  - [ ] 14.2 Implement skill relevance scoring and injection
  - [ ] 14.3 Implement auto-save skill draft
  - [ ] 14.4 Implement skills HTTP routes
  - [ ] 14.5 Unit tests for skills system

- [ ] 15. Web search
  - Implement `internal/search/client.go`: multi-provider search with fallback chain
  - Implement providers: SearXNG, DuckDuckGo (HTML scrape), Brave API, Google PSE, Tavily, Serper
  - Implement content fetcher: HTTP GET → extract text (strip HTML tags)
  - Implement TTL result cache
  - Implement `web_search` tool integration
  - Implement search routes: GET `/api/search`
  - Unit tests: provider fallback, cache hit/miss, content extraction
  - References: Requirement 18

  - [ ] 15.1 Implement SearXNG and DuckDuckGo providers
  - [ ] 15.2 Implement Brave, Google PSE, Tavily, Serper providers
  - [ ] 15.3 Implement fallback chain logic
  - [ ] 15.4 Implement content fetcher and HTML text extraction
  - [ ] 15.5 Implement TTL cache
  - [ ] 15.6 Unit tests for search providers and fallback

- [ ] 16. RAG (personal documents)
  - Implement `internal/memory/rag.go`: PDF/text upload → chunk → embed → store in ChromaDB
  - Implement RAG retrieval: query ChromaDB → prepend chunks to LLM context
  - Implement personal docs routes: POST `/api/personal-docs/upload`, GET/DELETE `/api/personal-docs`
  - Unit tests: chunking, retrieval, fallback to keyword search
  - References: Requirement 19

  - [ ] 16.1 Implement document chunking and embedding pipeline
  - [ ] 16.2 Implement RAG retrieval and context injection
  - [ ] 16.3 Implement personal docs HTTP routes
  - [ ] 16.4 Unit tests for RAG pipeline

- [ ] 17. Model endpoint management
  - Implement `internal/db` model endpoint CRUD
  - Implement model discovery: probe `/v1/models`, cache results, track hidden models
  - Implement endpoint routes: GET/POST/PUT/DELETE `/api/model-endpoints`, `/api/model-endpoints/{id}/probe`
  - Implement per-user endpoint visibility (owner-scoped + admin sees all)
  - References: Requirement 17

  - [ ] 17.1 Implement model endpoint CRUD in DB
  - [ ] 17.2 Implement model discovery and hidden model tracking
  - [ ] 17.3 Implement endpoint HTTP routes
  - [ ] 17.4 Implement per-user visibility filtering
  - [ ] 17.5 Integration tests for endpoint management

- [ ] 18. MCP server management
  - Implement `internal/mcp/manager.go`: connect stdio/SSE servers, enumerate tools, call tools
  - Use `mark3labs/mcp-go` for protocol; implement fallback hand-rolled client if unstable
  - Implement per-server disabled tool list
  - Implement MCP routes: GET/POST/PUT/DELETE `/api/mcp`, `/api/mcp/{id}/reconnect`
  - Unit tests: tool enumeration, disabled tool filtering, call routing
  - References: Requirement 16

  - [ ] 18.1 Implement MCP stdio transport client
  - [ ] 18.2 Implement MCP SSE transport client
  - [ ] 18.3 Implement tool enumeration and disabled tool filtering
  - [ ] 18.4 Implement MCP HTTP routes
  - [ ] 18.5 Unit tests for MCP manager

- [ ] 19. Compare (model A/B)
  - Implement compare routes: POST `/api/compare/start`, POST `/api/compare/vote`, GET `/api/compare/history`
  - Create two ephemeral sessions, stream both in parallel, blind mapping
  - Persist comparison record with prompt, responses, metrics, vote
  - References: Requirement 7

  - [ ] 19.1 Implement compare start (create ephemeral sessions, blind mapping)
  - [ ] 19.2 Implement parallel streaming for both models
  - [ ] 19.3 Implement vote recording and model reveal
  - [ ] 19.4 Implement comparison history route
  - [ ] 19.5 Integration tests for compare flow

- [ ] 20. Deep research
  - Implement `internal/research/engine.go`: Think→Search→Extract→Synthesize loop
  - Port all prompts from `src/deep_research.py` verbatim
  - Implement `internal/research/report.go`: HTML report generator (port of `src/visual_report.py`)
  - Implement research routes: POST `/api/research/start`, GET `/api/research/{id}/stream`, DELETE `/api/research/{id}/cancel`, GET `/api/research/{id}/report`
  - Integration tests: mock LLM + search → assert report produced, cancel works
  - References: Requirement 6

  - [ ] 20.1 Implement research engine loop with SSE progress
  - [ ] 20.2 Port all research prompts from Python
  - [ ] 20.3 Implement HTML report generator
  - [ ] 20.4 Implement research HTTP routes
  - [ ] 20.5 Integration tests for research flow

- [ ] 21. Notes & tasks
  - Implement notes CRUD routes: GET/POST/PUT/DELETE `/api/notes`
  - Implement `manage_notes` agent tool
  - Implement scheduled tasks CRUD routes: GET/POST/PUT/DELETE `/api/tasks`, `/api/tasks/{id}/run`
  - Implement task scheduler: background goroutine, cron/once/daily/weekly/monthly scheduling, chain execution
  - Implement task run persistence in `task_runs`
  - Implement reminder dispatch: browser push, email, ntfy
  - References: Requirement 13

  - [ ] 21.1 Implement notes CRUD routes and manage_notes tool
  - [ ] 21.2 Implement scheduled tasks CRUD routes
  - [ ] 21.3 Implement task scheduler background goroutine
  - [ ] 21.4 Implement task run persistence and chaining
  - [ ] 21.5 Implement reminder dispatch (browser/email/ntfy)
  - [ ] 21.6 Integration tests for scheduler

- [ ] 22. Calendar
  - Implement calendar CRUD routes: GET/POST/PUT/DELETE `/api/calendars`, `/api/calendars/{id}/events`
  - Implement `manage_calendar` agent tool
  - Implement CalDAV sync: pull remote calendars/events, upsert by UID, delete removed events
  - Implement `.ics` import/export
  - Implement periodic CalDAV sync (every 15 minutes via scheduler)
  - References: Requirement 12

  - [ ] 22.1 Implement calendar and event CRUD routes
  - [ ] 22.2 Implement manage_calendar agent tool
  - [ ] 22.3 Implement CalDAV pull sync
  - [ ] 22.4 Implement .ics import/export
  - [ ] 22.5 Implement periodic sync via scheduler
  - [ ] 22.6 Integration tests for calendar and CalDAV sync

- [ ] 23. Email
  - Implement `internal/email/imap.go`: connect, list folders, list messages, fetch message, mark read, delete, archive, bulk operations
  - Implement `internal/email/smtp.go`: send with threading headers, attachment support
  - Implement `internal/email/poller.go`: background polling loop per account, auto-summarize/tag/urgency
  - Implement email agent tools: list_emails, read_email, send_email, reply_to_email, bulk_email, archive_email, delete_email, mark_email_read
  - Implement email routes: GET/POST `/api/email/accounts`, GET `/api/email/messages`, POST `/api/email/send`, etc.
  - Implement attachment text extraction (PDF, images via vision model)
  - References: Requirement 11

  - [ ] 23.1 Implement IMAP client (connect, list, fetch, mark, delete, bulk)
  - [ ] 23.2 Implement SMTP client with threading headers
  - [ ] 23.3 Implement email poller with AI triage
  - [ ] 23.4 Implement all email agent tools
  - [ ] 23.5 Implement email HTTP routes
  - [ ] 23.6 Implement attachment text extraction
  - [ ] 23.7 Integration tests for email flow

- [ ] 24. Gallery & image editor
  - Implement gallery routes: GET/POST/DELETE `/api/gallery`, `/api/gallery/upload`, `/api/gallery/{id}`
  - Implement album CRUD routes
  - Implement EXIF extraction on upload (`rwcarlsen/goexif`)
  - Implement SHA-256 dedup on upload
  - Implement image generation proxy route: POST `/api/image/generate`
  - Implement editor draft CRUD routes: GET/POST/PUT/DELETE `/api/editor-drafts`
  - References: Requirement 14

  - [ ] 24.1 Implement gallery image CRUD and upload with EXIF + dedup
  - [ ] 24.2 Implement album CRUD routes
  - [ ] 24.3 Implement image generation proxy
  - [ ] 24.4 Implement editor draft CRUD routes
  - [ ] 24.5 Integration tests for gallery

- [ ] 25. Cookbook (model serving)
  - Implement `internal/cookbook/hardware.go`: GPU VRAM detection (nvidia-smi), RAM (/proc/meminfo), CPU count
  - Implement `internal/cookbook/catalog.go`: load embedded `hf_models.json`, compute fit scores
  - Implement `internal/cookbook/serve.go`: tmux session management for download/serve commands
  - Implement `internal/cookbook/ssh.go`: SSH command execution for remote servers
  - Implement cookbook routes: GET `/api/cookbook/hardware`, GET `/api/cookbook/models`, POST `/api/cookbook/download`, POST `/api/cookbook/serve`, DELETE `/api/cookbook/serve/{id}`
  - References: Requirement 15

  - [ ] 25.1 Implement hardware detection (GPU, RAM, CPU)
  - [ ] 25.2 Implement model catalog loading and fit scoring
  - [ ] 25.3 Implement tmux session management for download/serve
  - [ ] 25.4 Implement SSH remote server support
  - [ ] 25.5 Implement cookbook HTTP routes
  - [ ] 25.6 Integration tests for cookbook flow

- [ ] 26. Shell execution routes
  - Implement shell routes: POST `/api/shell/exec` (PTY streaming SSE), POST `/api/shell/tmux`
  - Enforce admin-only access
  - Implement EXEC_TIMEOUT (30s) and STREAM_TIMEOUT (120s)
  - References: Requirement 21

  - [ ] 26.1 Implement PTY shell exec with SSE streaming
  - [ ] 26.2 Implement tmux session tail route
  - [ ] 26.3 Enforce admin-only and timeout
  - [ ] 26.4 Integration tests for shell routes

- [ ] 27. TTS / STT
  - Implement `internal/tts/service.go`: provider abstraction (disabled, browser, OpenAI TTS, Kokoro local)
  - Implement `internal/tts/stt.go`: provider abstraction (disabled, browser, Whisper local, API endpoint)
  - Implement TTS routes: POST `/api/tts/synthesize`, GET `/api/tts/stats`
  - Implement STT routes: POST `/api/stt/transcribe`, GET `/api/stt/stats`
  - References: Requirement 20

  - [ ] 27.1 Implement TTS provider abstraction and routes
  - [ ] 27.2 Implement STT provider abstraction and routes
  - [ ] 27.3 Unit tests for provider selection and 503 on unavailable

- [ ] 28. Contacts
  - Implement contacts routes: GET `/api/contacts`, POST `/api/contacts`, GET `/api/contacts/search`
  - Implement CardDAV fetch when configured
  - Implement local `data/contacts.json` fallback
  - Implement `resolve_contact` and `manage_contact` agent tools
  - References: Requirement 22

  - [ ] 28.1 Implement local contacts CRUD
  - [ ] 28.2 Implement CardDAV fetch
  - [ ] 28.3 Implement contacts HTTP routes and agent tools
  - [ ] 28.4 Unit tests for contact resolution

- [ ] 29. Vault integration
  - Implement vault routes: GET/POST `/api/vault/config`, POST `/api/vault/unlock`
  - Implement `bw` CLI wrapper for secret retrieval
  - Store BW_SESSION in `data/vault.json` (mode 0600)
  - References: Requirement 23

  - [ ] 29.1 Implement vault config storage and bw CLI wrapper
  - [ ] 29.2 Implement vault HTTP routes
  - [ ] 29.3 Unit tests for vault config

- [ ] 30. Webhooks & API tokens
  - Implement webhook CRUD routes: GET/POST/PUT/DELETE `/api/webhooks`
  - Implement `internal/webhook/manager.go`: fire webhooks on events, HMAC-SHA256 signing, record delivery status
  - Implement API token CRUD routes: GET/POST/DELETE `/api/tokens`
  - Implement token creation (return full token once, store bcrypt hash + prefix)
  - References: Requirement 25

  - [ ] 30.1 Implement webhook CRUD and delivery with HMAC signing
  - [ ] 30.2 Implement API token CRUD with bcrypt hash storage
  - [ ] 30.3 Implement webhook event firing from chat/agent completion
  - [ ] 30.4 Unit tests for HMAC signing and token validation

- [ ] 31. Backup & restore
  - Implement backup routes: GET `/api/export`, POST `/api/import`
  - Export: memories, presets, skills, settings, features, preferences as JSON
  - Import: merge with existing, preserve existing records
  - References: Requirement 24

  - [ ] 31.1 Implement export route
  - [ ] 31.2 Implement import route with merge logic
  - [ ] 31.3 Unit tests for export/import round-trip

- [ ] 32. Remaining admin routes
  - Implement diagnostics route: GET `/api/diagnostics` (DB stats, ChromaDB status, MCP status, memory usage)
  - Implement admin wipe route: DELETE `/api/admin/wipe` (clear all user data)
  - Implement cleanup route: POST `/api/cleanup` (orphaned files, old sessions)
  - Implement health/version routes: GET `/api/health`, GET `/api/version`
  - Implement preferences routes: GET/POST `/api/prefs`
  - Implement presets routes: GET/POST/PUT/DELETE `/api/presets`
  - Implement emoji/font routes (static data endpoints)
  - References: Requirements 30, 24

  - [ ] 32.1 Implement diagnostics, health, version routes
  - [ ] 32.2 Implement admin wipe and cleanup routes
  - [ ] 32.3 Implement preferences and presets routes
  - [ ] 32.4 Implement emoji and font routes

- [ ] 33. Security hardening
  - Implement `SecurityHeadersMiddleware`: X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP
  - Implement `RequestTimeoutMiddleware`: 45s hard timeout with exempt prefixes
  - Implement prompt injection detection (port of `src/prompt_security.py`)
  - Implement rate limiter (per-user daily message count)
  - Implement internal-tool loopback token validation
  - References: Requirement 27

  - [ ] 33.1 Implement security headers middleware
  - [ ] 33.2 Implement request timeout middleware with exempt prefixes
  - [ ] 33.3 Implement prompt injection detection
  - [ ] 33.4 Implement rate limiter
  - [ ] 33.5 Implement internal-tool loopback bypass
  - [ ] 33.6 Unit tests for all security middleware

- [ ] 34. API compatibility validation
  - For each major API route, write a golden-file test comparing Go JSON response shape to the Python original
  - Run the Python server, capture response fixtures, store as `testdata/*.json`
  - Assert Go responses match fixtures (field names, types, nullability)
  - References: Requirement 29

  - [ ] 34.1 Capture Python response fixtures for all major routes
  - [ ] 34.2 Write golden-file comparison tests
  - [ ] 34.3 Fix any shape mismatches found

- [ ] 35. Docker & deployment
  - Write `Dockerfile` for Go binary (multi-stage: build → distroless/alpine)
  - Write `docker-compose.yml` mirroring the Python original (Theseus + ChromaDB + SearXNG + ntfy)
  - Write `install-service.sh` for systemd service
  - Write `.env.example` with all supported env vars
  - References: Requirement 1

  - [ ] 35.1 Write multi-stage Dockerfile
  - [ ] 35.2 Write docker-compose.yml
  - [ ] 35.3 Write systemd service file and install script
  - [ ] 35.4 Write .env.example
  - [ ] 35.5 Smoke test: docker compose up → frontend loads, login works
