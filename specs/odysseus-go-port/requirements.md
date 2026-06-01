# Requirements Document

## Introduction

Theseus is a full Go port of Odysseus — a self-hosted AI workspace. The goal is to replicate every feature of the Python/FastAPI Odysseus codebase in idiomatic Go, producing a single statically-linked binary that is easier to deploy, faster to start, and more resource-efficient than the Python original. The frontend (HTML/CSS/JS) is reused as-is; only the backend changes.

Odysseus features to port:
- Multi-user authentication with bcrypt, session tokens, 2FA (TOTP), and per-user privileges
- Multi-turn chat with streaming (SSE) against OpenAI-compatible LLM endpoints
- Autonomous agent loop with fenced-code-block tool dispatch
- Deep Research (iterative Think→Search→Extract→Synthesize)
- Model Compare (blind A/B side-by-side)
- Document editor (multi-tab, versioned, AI-assisted)
- Memory system (keyword + vector via ChromaDB)
- Skills system (SKILL.md files, auto-save, injection)
- Email (IMAP/SMTP, AI triage, drafts, bulk actions)
- Calendar (local SQLite + CalDAV sync)
- Notes & Tasks (reminders, checklists, scheduled agent tasks)
- Gallery (photo library, AI image generation, editor drafts)
- Cookbook (hardware-aware model catalog, download, serve via vLLM/llama.cpp)
- MCP server management (stdio + SSE transports)
- Model endpoint management (multi-provider, auto-discovery)
- Contacts (CardDAV + local JSON)
- Vault (Bitwarden/Vaultwarden CLI integration)
- Backup/restore (JSON export/import)
- Webhooks & API tokens
- TTS / STT (multi-provider)
- Shell execution (PTY streaming, tmux for long-running)
- Web search (SearXNG, DuckDuckGo, Brave, Google PSE, Tavily, Serper)
- RAG (personal docs, ChromaDB vector store)
- Settings & feature flags
- PWA / mobile-responsive static frontend (unchanged)

---

## Requirements

### Requirement 1 — Project Structure & Build

**User Story:** As a developer, I want a single `go build` to produce a self-contained binary that serves the full Odysseus feature set, so that deployment is a single file copy.

#### Acceptance Criteria

WHEN `go build ./cmd/theseus` is run THEN the system SHALL produce a single binary with no external Python runtime dependency.
WHEN the binary starts THEN it SHALL serve the existing `static/` frontend unchanged on the configured port (default 7000).
WHEN the binary starts THEN it SHALL auto-create `data/` subdirectories and run all DB migrations idempotently.
IF `DATABASE_URL` is not set THEN the system SHALL default to `sqlite://./data/app.db`.
WHEN the binary starts THEN it SHALL print the admin password on first boot if no users exist.

---

### Requirement 2 — Authentication & Authorization

**User Story:** As a user, I want to log in with a username and password (and optional 2FA), so that my data is private and the server is not open to the world.

#### Acceptance Criteria

WHEN a user POSTs valid credentials to `/api/auth/login` THEN the system SHALL set a secure HTTP-only session cookie valid for 7 days.
WHEN a user POSTs invalid credentials THEN the system SHALL return 401 and NOT set a cookie.
WHEN `AUTH_ENABLED=true` THEN the system SHALL reject all non-exempt API requests without a valid session cookie or Bearer API token.
WHEN `LOCALHOST_BYPASS=true` THEN the system SHALL allow unauthenticated loopback requests (127.0.0.1 / ::1).
WHEN a user enables 2FA THEN the system SHALL require a valid TOTP code on every login after that.
WHEN an admin creates a user THEN the system SHALL store a bcrypt hash of the password, never plaintext.
WHEN an admin deletes a user THEN the system SHALL immediately revoke all active session tokens for that user.
WHEN a non-admin user accesses an admin-only route THEN the system SHALL return 403.
WHEN `signup_enabled=true` THEN the system SHALL allow self-registration via `/api/auth/signup`.
WHEN a user's privilege `can_use_bash=false` THEN the system SHALL block shell tool calls for that user.
WHEN an API token is presented as `Authorization: Bearer <token>` THEN the system SHALL authenticate the request with the token's owner and scopes.
WHEN an API token is created or revoked THEN the system SHALL invalidate the in-memory token cache.

---

### Requirement 3 — Chat (Streaming)

**User Story:** As a user, I want to send messages to an LLM and receive a streaming response, so that I get fast, incremental output.

#### Acceptance Criteria

WHEN a user sends a chat message THEN the system SHALL stream the LLM response as SSE to the client.
WHEN a session has `rag=true` THEN the system SHALL prepend relevant memory/document context to the LLM request.
WHEN the LLM endpoint is unreachable THEN the system SHALL return a 503 with a human-readable error after 2 consecutive failures (dead-host cooldown).
WHEN a response completes THEN the system SHALL persist the assistant message to the DB anddate `last_message_at`.
WHEN a chat request includes an uploaded file THEN the system SHALL include it as a vision attachment if the model supports vision.
WHEN a session has `mode=agent` THEN the system SHALL route through the agent loop instead of plain chat.
WHEN a plain-chat message matches tool-intent patterns (e.g. "remind me", "add to calendar") THEN the system SHALL silently escalate to the agent loop.
WHEN a response is in progress and the client disconnects THEN the system SHALL save any partial response accumulated so far.
WHEN `max_messages_per_day > 0` for a user THEN the system SHALL enforce that daily message limit and return 429 when exceeded.

---

### Requirement 4 — Agent Loop

**User Story:** As a user, I want the AI to autonomously plan and execute multi-step tasks using tools, so that I can delegate complex work.

#### Acceptance Criteria

WHEN the agent loop is active THEN the system SHALL parse fenced code blocks (e.g. ` ```bash `) from LLM output and execute the corresponding tool.
WHEN a tool block executes THEN the system SHALL append the tool result to the conversation and continue the loop.
WHEN the loop reaches `MAX_AGENT_ROUNDS` (20) THEN the system SHALL stop and return the current state.
WHEN a tool times out (60s) THEN the system SHALL return a timeout error result and continue the loop.
WHEN the LLM emits OpenAI-style `tool_calls` (function calling) THEN the system SHALL dispatch them identically to fenced-block tools.
WHEN MCP servers are connected THEN the system SHALL expose their tools to the agent alongside built-in tools.
WHEN a user's privilege blocks a tool (e.g. `can_use_bash=false`) THEN the system SHALL skip that tool and return a permission-denied result.
WHEN the agent loop completes THEN the system SHALL stream all intermediate tool output and final response to the client via SSE.

---

### Requirement 5 — Built-in Agent Tools

**User Story:** As a user, I want the agent to have a rich set of built-in tools (shell, files, web, memory, calendar, email, etc.) so that it can act on my behalf across my workspace.

#### Acceptance Criteria

WHEN the agent calls `bash` THEN the system SHALL execute the command in a sandboxed shell with a 60s timeout and return stdout/stderr (max 10K chars).
WHEN the agent calls `python` THEN the system SHALL execute the code with a 30s timeout.
WHEN the agent calls `web_search` THEN the system SHALL query the configured search provider and return results.
WHEN the agent calls `read_file` / `write_file` THEN the system SHALL read/write files relative to the data directory.
WHEN the agent calls `create_document` / `edit_document` / `update_document` THEN the system SHALL create or modify a versioned document in the DB.
WHEN the agent calls `manage_memory` THEN the system SHALL add, search, or delete memory entries.
WHEN the agent calls `manage_notes` THEN the system SHALL create, update, or delete notes/checklists.
WHEN the agent calls `manage_calendar` THEN the system SHALL create, update, or delete calendar events.
WHEN the agent calls `send_email` / `list_emails` / `read_email` / `reply_to_email` / `bulk_email` THEN the system SHALL perform the corresponding IMAP/SMTP operation.
WHEN the agent calls `generate_image` THEN the system SHALL proxy the request to the configured image generation endpoint.
WHEN the agent calls `manage_tasks` THEN the system SHALL create, update, or delete scheduled tasks.
WHEN the agent calls `manage_skills` THEN the system SHALL add, update, or retrieve skills.
WHEN the agent calls `list_models` / `manage_endpoints` THEN the system SHALL return or modify model endpoint configuration.
WHEN the agent calls `app_api` THEN the system SHALL make an authenticated loopback HTTP call to any internal API route.

---

### Requirement 6 — Deep Research

**User Story:** As a user, I want to submit a research question and receive a cited, synthesized HTML report, so that I can quickly understand complex topics.

#### Acceptance Criteria

WHEN a user starts a research job THEN the system SHALL run an iterative Think→Search→Extract→Synthesize loop asynchronously.
WHEN the research loop runs THEN the system SHALL stream progress events (SSE) to the client.
WHEN the loop completes THEN the system SHALL produce a self-contained HTML report with citations, table of contents, and dark/light theme.
WHEN a research job is in progress THEN the system SHALL allow the user to cancel it.
WHEN the research endpoint or model is not configured THEN the system SHALL fall back to the session's endpoint/model.
WHEN a research job finishes THEN the system SHALL persist the report to the session's message history.

---

### Requirement 7 — Model Compare

**User Story:** As a user, I want to send one prompt to two models simultaneously and compare their responses side-by-side (optionally blind), so that I can evaluate model quality without bias.

#### Acceptance Criteria

WHEN a user starts a comparison THEN the system SHALL create two ephemeral sessions and stream both responses in parallel.
WHEN `is_blind=true` THEN the system SHALL randomize which model is shown as "A" vs "B" and not reveal model names until the user votes.
WHEN a user votes THEN the system SHALL record the winner in the DB and reveal the model identities.
WHEN a comparison is complete THEN the system SHALL store the full prompt, responses, metrics, and vote result.

---

### Requirement 8 — Document Editor

**User Story:** As a user, I want a multi-tab document editor where I can write text and ask the AI to make targeted edits, so that I stay in control of my content.

#### Acceptance Criteria

WHEN a user creates a document THEN the system SHALL store it with a title, language (markdown/html/python/etc.), and initial content.
WHEN the AI edits a document THEN the system SHALL create a new version snapshot and increment `version_count`.
WHEN a user requests a document list THEN the system SHALL return only documents owned by that user.
WHEN a document is archived THEN the system SHALL hide it from the default list but retain it for restore.
WHEN a document has `source_email_*` fields THEN the system SHALL support a "sign and reply" flow linking back to the source email.
WHEN a user exports a document THEN the system SHALL return the current content as a downloadable file.

---

### Requirement 9 — Memory System

**User Story:** As a user, I want the assistant to remember facts about me across conversations, so that it becomes more useful over time.

#### Acceptance Criteria

WHEN a conversation ends THEN the system SHALL optionally extract and persist memory entries (text, category, source, owner).
WHEN a chat request is processed THEN the system SHALL retrieve relevant memories via keyword + optional vector search and inject them into the system prompt.
WHEN ChromaDB is available THEN the system SHALL use vector embeddings for semantic memory retrieval.
WHEN ChromaDB is unavailable THEN the system SHALL fall back to keyword-only retrieval without crashing.
WHEN a user deletes a memory THEN the system SHALL remove it from both the SQL DB and the vector store.
WHEN a user imports memories THEN the system SHALL merge them with existing entries, deduplicating by text similarity.

---

### Requirement 10 — Skills System

**User Story:** As a user, I want the agent to learn and reuse procedural skills (SKILL.md files) so that it gets better at recurring tasks over time.

#### Acceptance Criteria

WHEN the agent completes a novel task THEN the system SHALL optionally auto-save a skill draft with a confidence score.
WHEN a skill's confidence meets `skill_autosave_min_confidence` THEN the system SHALL inject it into future agent prompts.
WHEN a user publishes a skill THEN the system SHALL always inject it regardless of confidence.
WHEN the agent prompt is built THEN the system SHALL inject at most `skill_max_injected` relevant skills.
WHEN a user lists skills THEN the system SHALL return skills filtered by owner.
WHEN a skill is deleted THEN the system SHALL remove its SKILL.md file from disk.

---

### Requirement 11 — Email

**User Story:** As a user, I want an IMAP/SMTP inbox with AI triage built in, so that I can manage email without leaving my workspace.

#### Acceptance Criteria

WHEN a user configures an email account THEN the system SHALL store IMAP/SMTP credentials encrypted at rest (Fernet/AES-GCM).
WHEN the email poller runs THEN the system SHALL fetch new messages, auto-summarize, auto-tag, and optionally flag urgency.
WHEN a user requests email list THEN the system SHALL return paginated messages with AI-generated summaries and tags.
WHEN a user sends a reply THEN the system SHALL send via SMTP with proper threading headers (In-Reply-To, References).
WHEN a user calls `bulk_email` THEN the system SHALL perform the action (delete/archive/mark-read) on all specified UIDs in one IMAP operation.
WHEN an email has attachments THEN the system SHALL extract text from PDFs and images for AI context.
WHEN a user has multiple email accounts THEN the system SHALL route operations to the correct account.
WHEN email passwords are stored THEN the system SHALL encrypt them and never return plaintext to the API.

---

### Requirement 12 — Calendar

**User Story:** As a user, I want a local calendar with CalDAV sync, so that my events are available in the workspace and stay in sync with external services.

#### Acceptance Criteria

WHEN a user creates a calendar THEN the system SHALL store it in SQLite with a name, color, and owner.
WHEN a user creates an event THEN the system SHALL store it with title, start/end (UTC), recurrence rule, and calendar reference.
WHEN CalDAV credentials are configured THEN the system SHALL pull remote calendars and events on open and on a periodic schedule.
WHEN a remote event is deleted THEN the system SHALL delete the corresponding local row on the next sync.
WHEN a user imports an `.ics` file THEN the system SHALL parse and upsert all VEVENT entries.
WHEN a user exports a calendar THEN the system SHALL produce a valid `.ics` file.

---

### Requirement 13 — Notes & Tasks

**User Story:** As a user, I want quick notes with reminders and a todo list, plus scheduled agent tasks, so that I can capture and act on things without switching apps.

#### Acceptance Criteria

WHEN a user creates a note THEN the system SHALL store it with title, content or checklist items, color, label, pin state, and due date.
WHEN a note has a `due_date` THEN the system SHALL fire a reminder via the configured channel (browser push, email, or ntfy).
WHEN a user creates a scheduled task THEN the system SHALL store schedule (once/daily/weekly/monthly/cron), model, prompt, and output target.
WHEN a scheduled task is due THEN the system SHALL run the agent loop with the task's prompt and persist the result.
WHEN a task run completes THEN the system SHALL record start time, finish time, status, result, and tokens used in `task_runs`.
WHEN a task has `then_task_id` THEN the system SHALL chain execution to the next task on success.

---

### Requirement 14 — Gallery & Image Editor

**User Story:** As a user, I want a photo library where I can upload, generate, and edit images, so that all my visual assets are in one place.

#### Acceptance Criteria

WHEN a user uploads an image THEN the system SHALL extract EXIF metadata, compute a SHA-256 hash for dedup, and store metadata in the DB.
WHEN a duplicate image is uploaded THEN the system SHALL return the existing record without creating a new one.
WHEN a user generates an image THEN the system SHALL proxy the request to the configured image endpoint and save the result to the gallery.
WHEN a user opens the editor THEN the system SHALL load or create an `EditorDraft` with layer state persisted as JSON.
WHEN a user saves an editor draft THEN the system SHALL update the payload and thumbnail in the DB.
WHEN a user organizes images into albums THEN the system SHALL support album CRUD with cover image selection.

---

### Requirement 15 — Cookbook (Model Serving)

**User Story:** As an admin, I want hardware-aware model recommendations and one-click download/serve, so that I can run local models without manual configuration.

#### Acceptance Criteria

WHEN the Cookbook page loads THEN the system SHALL return the hardware profile (VRAM, RAM, CPU) and a scored list of compatible models.
WHEN a user clicks download THEN the system SHALL start a `huggingface-cli` download in a tmux session and stream progress logs.
WHEN a user clicks serve THEN the system SHALL launch vLLM or llama.cpp in a tmux session with the appropriate flags and auto-register the endpoint.
WHEN a model is served THEN the system SHALL auto-register it as a `ModelEndpoint` in the DB.
WHEN a remote server is configured THEN the system SHALL execute download/serve commands over SSH.
WHEN a download is in progress THEN the system SHALL allow cancellation by killing the tmux session.

---

### Requirement 16 — MCP Server Management

**User Story:** As an admin, I want to connect external MCP tool servers (stdio or SSE), so that the agent has access to additional tools.

#### Acceptance Criteria

WHEN an MCP server is added THEN the system SHALL connect to it via stdio or SSE transport and enumerate its tools.
WHEN an MCP server is connected THEN the system SHALL make its tools available to the agent loop.
WHEN a tool on an MCP server is disabled THEN the system SHALL exclude it from the agent's tool list.
WHEN an MCP server disconnects THEN the system SHALL log the error and attempt reconnection on the next agent request.
WHEN an MCP server uses OAuth THEN the system SHALL support the configured OAuth flow for token acquisition.

---

### Requirement 17 — Model Endpoint Management

**User Story:** As an admin, I want to configure multiple LLM endpoints (local vLLM, Ollama, OpenRouter, OpenAI, etc.) and have models auto-discovered, so that users can pick from all available models.

#### Acceptance Criteria

WHEN an endpoint is added THEN the system SHALL probe `/v1/models` and cache the model list.
WHEN a model fails probing THEN the system SHALL add it to `hidden_models` and exclude it from the picker.
WHEN a user requests the model list THEN the system SHALL return models from all enabled endpoints the user has access to.
WHEN an endpoint has `supports_tools=true` THEN the system SHALL use OpenAI function-calling format for that endpoint.
WHEN an endpoint's API key is stored THEN the system SHALL encrypt it at rest.
WHEN an endpoint is owner-scoped THEN the system SHALL only show it to that user (admins see all).

---

### Requirement 18 — Web Search

**User Story:** As a user, I want the agent to search the web using my configured provider, so that it can find current information.

#### Acceptance Criteria

WHEN a web search is requested THEN the system SHALL query the primary provider (SearXNG, Brave, Google PSE, Tavily, Serper, or DuckDuckGo).
WHEN the primary provider fails THEN the system SHALL try each provider in `search_fallback_chain` in order.
WHEN search results are returned THEN the system SHALL fetch and extract text content from the top result URLs.
WHEN a search result URL is unreachable THEN the system SHALL skip it and continue with remaining results.
WHEN search results are cached THEN the system SHALL return cached results for identical queries within the TTL.

---

### Requirement 19 — RAG (Personal Documents)

**User Story:** As a user, I want to upload personal documents and have the AI reference them in chat, so that it can answer questions about my own files.

#### Acceptance Criteria

WHEN a user uploads a PDF or text file THEN the system SHALL extract text, chunk it, and store embeddings in ChromaDB.
WHEN a session has `rag=true` THEN the system SHALL retrieve relevant chunks and prepend them to the LLM context.
WHEN ChromaDB is unavailable THEN the system SHALL fall back to keyword search over stored chunks.
WHEN a user deletes a personal document THEN the system SHALL remove its chunks from the vector store.

---

### Requirement 20 — TTS / STT

**User Story:** As a user, I want text-to-speech and speech-to-text so that I can interact with the assistant by voice.

#### Acceptance Criteria

WHEN TTS is enabled and a response is generated THEN the system SHALL synthesize audio via the configured provider (Kokoro local, OpenAI TTS, or browser).
WHEN STT is enabled and audio is uploaded THEN the system SHALL transcribe it via the configured provider (local Whisper or API endpoint).
WHEN the TTS/STT provider is unavailable THEN the system SHALL return 503 with a clear error message.

---

### Requirement 21 — Shell Execution

**User Story:** As an admin, I want to run shell commands from the UI with streaming output, so that I can manage the server without SSH.

#### Acceptance Criteria

WHEN an admin submits a shell command THEN the system SHALL execute it in a PTY and stream output as SSE.
WHEN a command exceeds `EXEC_TIMEOUT` (30s) THEN the system SHALL kill the process and return a timeout error.
WHEN a non-admin user accesses shell routes THEN the system SHALL return 403.
WHEN a long-running command is needed (e.g. model download) THEN the system SHALL run it in a tmux session and tail the log.

---

### Requirement 22 — Contacts

**User Story:** As a user, I want to manage contacts (CardDAV or local JSON) so that the agent can resolve email addresses and names.

#### Acceptance Criteria

WHEN CardDAV is configured THEN the system SHALL fetch contacts from the remote server.
WHEN CardDAV is not configured THEN the system SHALL use a local `data/contacts.json` file.
WHEN the agent calls `resolve_contact` THEN the system SHALL search contacts by name or email and return matches.
WHEN a user adds a contact THEN the system SHALL persist it to the local store and optionally sync to CardDAV.

---

### Requirement 23 — Vault Integration

**User Story:** As an admin, I want to integrate with Bitwarden/Vaultwarden CLI so that the agent can retrieve secrets from the vault.

#### Acceptance Criteria

WHEN vault credentials are configured THEN the system SHALL store the BW_SESSION key in `data/vault.json` with mode 0600.
WHEN the agent needs a secret THEN the system SHALL call `bw get` with the stored session token.
WHEN the vault session expires THEN the system SHALL prompt for re-authentication.

---

### Requirement 24 — Backup & Restore

**User Story:** As an admin, I want to export and import all user data (memories, skills, presets, settings, preferences) as a JSON file, so that I can migrate or restore the workspace.

#### Acceptance Criteria

WHEN an admin requests export THEN the system SHALL produce a JSON file containing memories, presets, skills, settings, features, and preferences.
WHEN an admin imports a backup THEN the system SHALL merge the data, preserving existing records and adding new ones.
WHEN a backup file has an unknown version THEN the system SHALL reject it with a clear error.

---

### Requirement 25 — Webhooks & API Tokens

**User Story:** As an admin, I want outgoing webhooks and API tokens so that external systems can integrate with Odysseus.

#### Acceptance Criteria

WHEN a webhook is configured for an event THEN the system SHALL POST a signed (HMAC-SHA256) payload to the webhook URL when that event fires.
WHEN a webhook delivery fails THEN the system SHALL record the status code and error in the DB.
WHEN an API token is created THEN the system SHALL return the full token once and store only a bcrypt hash with a prefix for display.
WHEN an API token is used THEN the system SHALL update `last_used_at` and enforce the token's scopes.

---

### Requirement 26 — Settings & Feature Flags

**User Story:** As an admin, I want a settings panel to configure all integrations and feature flags without editing files, so that the workspace is easy to manage.

#### Acceptance Criteria

WHEN settings are saved THEN the system SHALL persist them to `data/settings.json` atomically.
WHEN a feature flag is toggled THEN the system SHALL persist it to `data/features.json` and take effect within 2 seconds (TTL cache).
WHEN settings are read on a hot path THEN the system SHALL serve from an in-memory cache with a 2-second TTL.
WHEN the app starts THEN the system SHALL merge saved settings with defaults so new keys are always present.

---

### Requirement 27 — Security

**User Story:** As an operator, I want the server to follow security best practices so that it is safe to expose on a LAN or behind a reverse proxy.

#### Acceptance Criteria

WHEN any response is sent THEN the system SHALL include security headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy, CSP).
WHEN secrets (API keys, email passwords, signatures) are stored THEN the system SHALL encrypt them with AES-GCM (or equivalent) using a key stored at `data/.app_key` (mode 0600).
WHEN a request body contains a prompt THEN the system SHALL apply prompt-injection detection and prepend an untrusted-context warning when external content is included.
WHEN a hard request timeout (45s default) is exceeded THEN the system SHALL return 504 (exempt: streaming, research, downloads).
WHEN rate limiting is configured THEN the system SHALL enforce per-user message limits.
WHEN an internal tool loopback call is made THEN the system SHALL validate the internal token and restrict to loopback clients only.

---

### Requirement 28 — Data Persistence & Migrations

**User Story:** As an operator, I want the database schema to migrate automatically on startup so that upgrades are zero-downtime.

#### Acceptance Criteria

WHEN the binary starts THEN the system SHALL run all pending schema migrations idempotently.
WHEN a new column is added THEN the system SHALL use `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` (or equivalent) so existing data is preserved.
WHEN the DB file does not exist THEN the system SHALL create it and run all migrations from scratch.
WHEN a migration fails THEN the system SHALL log the error and continue (non-fatal for additive migrations).

---

### Requirement 29 — PWA & Frontend Compatibility

**User Story:** As a user, I want the Go backend to serve the existing Odysseus frontend unchanged, so that the UI experience is identical.

#### Acceptance Criteria

WHEN the binary starts THEN the system SHALL serve `static/` as a file tree with correct MIME types.
WHEN a non-API path is requested THEN the system SHALL return `static/index.html` (SPA fallback).
WHEN `/login` is requested THEN the system SHALL return `static/login.html`.
WHEN the frontend makes any API call THEN the system SHALL respond with the same JSON shape as the Python original.
WHEN the service worker (`sw.js`) is served THEN the system SHALL set `Service-Worker-Allowed: /` header.

---

### Requirement 30 — Observability & Health

**User Story:** As an operator, I want health and diagnostic endpoints so that I can monitor the service.

#### Acceptance Criteria

WHEN `/api/health` is called THEN the system SHALL return `{"status": "ok"}` with 200.
WHEN `/api/version` is called THEN the system SHALL return the build version and Go runtime info.
WHEN `/api/diagnostics` is called by an admin THEN the system SHALL return DB stats, ChromaDB connectivity, MCP server statuses, and memory usage.
WHEN the application logs THEN the system SHALL use structured JSON logging with level, timestamp, and request ID.
