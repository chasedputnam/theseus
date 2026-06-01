# Theseus

A self-hosted AI workspace — chat, agents, research, email, calendar, notes, and more — running entirely on your own hardware. No subscriptions, no telemetry, no cloud dependency.

Theseus is a Go port of [Odysseus](https://github.com/pewdiepie-archdaemon/odysseus), rewritten as a single self-contained binary that is faster to start, easier to deploy, and simpler to maintain.

---

## Table of Contents

- [What Theseus Does](#what-theseus-does)
- [Installation](#installation)
  - [Option 1: Docker (recommended)](#option-1-docker-recommended)
  - [Option 2: Single binary (Linux / macOS)](#option-2-single-binary-linux--macos)
  - [Option 3: Build from source](#option-3-build-from-source)
- [First-Time Setup](#first-time-setup)
- [Configuration](#configuration)
- [Features](#features)
  - [Chat](#chat)
  - [Agent](#agent)
  - [Deep Research](#deep-research)
  - [Compare](#compare)
  - [Documents](#documents)
  - [Memory](#memory)
  - [Skills](#skills)
  - [Email](#email)
  - [Calendar](#calendar)
  - [Notes & Tasks](#notes--tasks)
  - [Gallery & Image Editor](#gallery--image-editor)
  - [Cookbook (Model Serving)](#cookbook-model-serving)
  - [Web Search](#web-search)
  - [Contacts](#contacts)
  - [Vault](#vault)
  - [Shell](#shell)
  - [TTS / STT](#tts--stt)
- [Putting Theseus Behind HTTPS](#putting-theseus-behind-https)
- [Backup & Restore](#backup--restore)
- [Updating](#updating)
- [Important Considerations](#important-considerations)

---

## What Theseus Does

Theseus gives you a private, browser-based workspace for working with AI language models. You point it at any OpenAI-compatible endpoint — a local model running on your machine, a self-hosted server, or a cloud API — and it handles everything else: conversation history, file uploads, autonomous agents, email triage, calendar sync, and more.

Everything stays on your machine. Your conversations, memories, documents, and credentials never leave your server unless you explicitly connect an external service.

---

## Installation

### Option 1: Docker (recommended)

Docker is the easiest way to run Theseus. It starts Theseus alongside ChromaDB (vector memory), SearXNG (web search), and ntfy (notifications) automatically.

**Requirements:** Docker and Docker Compose installed.

```bash
git clone https://github.com/chaseputnam/theseus.git
cd theseus
cp .env.example .env        # optional — defaults work out of the box
docker compose up -d --build
```

Open `http://localhost:7000` in your browser. The first time it starts, Theseus prints a generated admin password to the logs:

```bash
docker compose logs theseus | grep "admin password"
```

Log in with username `admin` and that password, then change it in **Settings → Account**.

**Useful commands:**

```bash
docker compose ps                          # check status
docker compose logs --tail=100 theseus    # view logs
docker compose down                        # stop everything
docker compose pull && docker compose up -d --build  # update
```

---

### Option 2: Single binary (Linux / macOS)

Download the latest release binary from the [Releases page](https://github.com/chaseputnam/theseus/releases).

```bash
# Download and make executable
chmod +x theseus

# Create a data directory
mkdir -p data

# Run
./theseus --port 7000 --data-dir ./data --static-dir ./static
```

The first run prints a generated admin password to the terminal. Open `http://localhost:7000` and log in.

**Run as a background service (Linux systemd):**

```bash
sudo cp theseus /opt/theseus/
sudo cp theseus.service /etc/systemd/system/
sudo systemctl enable --now theseus
sudo journalctl -u theseus -f   # view logs
```

---

### Option 3: Build from source

**Requirements:** Go 1.22 or later.

```bash
git clone https://github.com/chaseputnam/theseus.git
cd theseus
go build -o theseus ./cmd/theseus
./theseus --port 7000 --data-dir ./data --static-dir ./static
```

---

## First-Time Setup

1. **Log in** with username `admin` and the generated password shown in the logs.
2. **Change your password** — go to the top-right menu → **Account** → **Change Password**.
3. **Add a model endpoint** — go to **Settings → Model Endpoints → Add Endpoint**. Enter the base URL of your LLM server (e.g. `http://localhost:8000` for a local vLLM or Ollama instance, or `https://api.openai.com` for OpenAI). Add an API key if required.
4. **Pick a default model** — go to **Settings → Default Model** and select the model you want to use for chat.
5. **Configure web search** (optional) — if you used Docker, SearXNG is already running at `http://searxng:8080` and pre-configured. For a manual install, set `SEARXNG_INSTANCE` in your `.env` or point to any SearXNG instance you have access to.

That's it. You're ready to start chatting.

---

## Configuration

Most configuration is done inside the app under **Settings**. The `.env` file (or environment variables) is only needed for deployment-level settings that must be present before the app starts.

| Variable | Default | What it does |
|---|---|---|
| `PORT` | `7000` | Port the server listens on |
| `DATA_DIR` | `data` | Directory where all user data is stored |
| `STATIC_DIR` | `static` | Directory containing the frontend files |
| `AUTH_ENABLED` | `true` | Set to `false` to disable login (dev only) |
| `LOCALHOST_BYPASS` | `false` | Allow unauthenticated requests from 127.0.0.1 (dev only) |
| `DATABASE_URL` | `data/app.db` | SQLite database path |
| `CHROMADB_HOST` | _(empty)_ | ChromaDB host for vector memory (Docker sets this automatically) |
| `CHROMADB_PORT` | `8100` | ChromaDB port |
| `SEARXNG_INSTANCE` | _(empty)_ | SearXNG URL for web search |

Everything else — LLM endpoints, email accounts, calendar sync, notification channels, TTS/STT providers — is configured inside the app.

---

## Features

### Chat

The core of Theseus. Start a conversation with any model on any of your configured endpoints. Each conversation is saved automatically and searchable later.

**How to use:**
- Click **New Chat** in the sidebar.
- Select your model from the dropdown at the top.
- Type your message and press Enter.

**Tips:**
- You can upload images, PDFs, and text files directly into the chat. The model will read them if it supports vision or document input.
- Switch between **Chat** mode (plain conversation) and **Agent** mode (the AI can use tools) using the toggle next to the send button.
- Rename, star, archive, or delete conversations from the sidebar context menu.
- Organize conversations into folders by right-clicking a session.

---

### Agent

In Agent mode, the AI can take actions on your behalf: run shell commands, search the web, read and write files, create documents, send emails, manage your calendar, generate images, and more.

**How to use:**
- Toggle **Agent** mode in any chat session.
- Ask the AI to do something: *"Search the web for the latest Go release notes and summarize them"* or *"Create a document with a project plan for building a REST API"*.
- The AI will show you each tool it uses and the result before continuing.

**Available tools:**
- `bash` / `python` — run commands and scripts (admin only)
- `web_search` — search the web
- `read_file` / `write_file` — read and write files in your data directory
- `create_document` / `edit_document` — create and edit documents
- `manage_memory` — add, search, or delete memories
- `manage_notes` / `manage_calendar` — create notes and calendar events
- `send_email` / `list_emails` / `read_email` — manage your email
- `generate_image` — generate images via your configured image endpoint
- `manage_tasks` — create and manage scheduled tasks
- Any tool from connected MCP servers

---

### Deep Research

Give Theseus a question and it will run a multi-step research loop: generating search queries, fetching and reading sources, synthesizing findings, and producing a polished HTML report with citations.

**How to use:**
- Click **Research** in the sidebar.
- Type your research question and click **Start Research**.
- Watch the progress as it searches and synthesizes. This typically takes 1–5 minutes.
- The finished report opens in a new tab and can be saved or shared.

**Tips:**
- Research works best with a capable model (7B+ parameters recommended).
- You can configure a separate model specifically for research under **Settings → Research Model**.

---

### Compare

Send one prompt to two different models simultaneously and compare their responses side by side — optionally in blind mode so you can judge quality without knowing which model wrote which response.

**How to use:**
- Click **Compare** in the sidebar.
- Select two models and enter your prompt.
- Both responses stream in parallel.
- In blind mode, vote for the better response — the model identities are revealed after you vote.

---

### Documents

A multi-tab document editor where you write the content and the AI assists on request. Supports Markdown, HTML, Python, JavaScript, SQL, JSON, and plain text with syntax highlighting.

**How to use:**
- Click **Documents** in the sidebar.
- Click **New Document** to create one, or open an existing document.
- Write normally. To get AI help, select text and click **Ask AI**, or type a request in the chat panel on the right.
- The AI uses targeted FIND/REPLACE edits — it changes only what you ask, not the whole document.
- Every AI edit creates a version snapshot. Click the version history icon to see or restore previous versions.

---

### Memory

Theseus can remember facts about you across conversations. Memories are stored in your database and injected into the AI's context when relevant.

**How to use:**
- Memories are extracted automatically from conversations when the AI notices something worth remembering.
- You can also add memories manually: go to **Settings → Memory → Add Memory**.
- Search, edit, or delete memories from the Memory panel.
- If ChromaDB is running, memories use semantic (vector) search. Otherwise they use keyword search.

---

### Skills

Skills are reusable procedures the AI learns over time. When the agent successfully completes a novel task, it can save a SKILL.md file describing how it did it. Future agent runs automatically load relevant skills.

**How to use:**
- Skills are managed under **Settings → Skills**.
- You can write skills manually, edit AI-generated drafts, and publish skills to make them always available.
- Published skills are always injected into the agent prompt. Draft skills are only injected if their confidence score meets the threshold in **Settings → Skill Settings**.

---

### Email

Connect your IMAP/SMTP email accounts and manage them from within Theseus. The AI can summarize messages, draft replies in your writing style, auto-tag, and flag urgent emails.

**How to use:**
- Go to **Settings → Email → Add Account** and enter your IMAP/SMTP credentials.
- Open **Email** in the sidebar to see your inbox.
- Click a message to read it. The AI summary appears at the top.
- Click **Draft Reply** to have the AI write a reply in your style.
- Use **Bulk Actions** to archive, delete, or mark multiple messages at once.

**Supported providers:** Any IMAP/SMTP server — Gmail, Outlook, Fastmail, ProtonMail Bridge, self-hosted, etc.

**Note:** Email passwords are encrypted at rest using AES-256-GCM.

---

### Calendar

A local calendar with optional CalDAV sync to Radicale, Nextcloud, Apple Calendar, Fastmail, or any CalDAV-compatible server.

**How to use:**
- Open **Calendar** in the sidebar.
- Click any day to create an event, or click an existing event to edit it.
- To sync with an external calendar, go to **Settings → Calendar → CalDAV** and enter your server URL and credentials.
- Import `.ics` files via **Calendar → Import**.
- Export any calendar as `.ics` via the calendar's context menu.

---

### Notes & Tasks

Quick notes with reminders, checklists, and scheduled agent tasks.

**Notes:**
- Open **Notes** in the sidebar.
- Click **+** to create a note. Notes can be plain text or checklists.
- Set a due date to get a reminder via browser notification, email, or ntfy.
- Pin important notes to keep them at the top.

**Scheduled Tasks:**
- Go to **Tasks** in the sidebar.
- Create a task with a prompt, schedule (once / daily / weekly / monthly / cron), and model.
- At the scheduled time, Theseus runs the agent with your prompt and saves the result to a session.
- View run history and results from the task detail page.

---

### Gallery & Image Editor

A photo library for uploaded and AI-generated images, with a layered editor for inpainting, background removal, and compositing.

**How to use:**
- Open **Gallery** in the sidebar.
- Upload photos by dragging them in, or generate images via the agent (`generate_image` tool) or the **Generate** button.
- Organize images into albums.
- Click **Edit** on any image to open the layered editor.
- Editor drafts are saved automatically so you can close and resume later.

**Image generation** requires a configured image endpoint (e.g. a local Stable Diffusion server or DALL-E via OpenAI). Set this under **Settings → Image Generation**.

---

### Cookbook (Model Serving)

Hardware-aware model recommendations and one-click download and serving for local models.

**How to use:**
- Open **Cookbook** in the sidebar.
- Theseus scans your hardware (GPU VRAM, RAM, CPU) and shows a scored list of compatible models from the catalog of 270+ models.
- Click **Download** next to a model to start downloading it via `huggingface-cli`.
- Click **Serve** to launch the model with vLLM or llama.cpp. Theseus automatically registers the served model as an endpoint.
- Monitor download and serve progress from the Cookbook log panel.

**Requirements:** `huggingface-cli` for downloads; `vllm` or `llama-server` for serving; `tmux` for background processes.

---

### Web Search

Theseus can search the web using several providers. The agent uses web search automatically when it needs current information.

**Supported providers:** SearXNG (default with Docker), DuckDuckGo (free, no key), Brave Search, Google Programmable Search Engine, Tavily, Serper.

**How to configure:**
- Go to **Settings → Search**.
- Select your primary provider and enter any required API keys.
- Set a fallback chain — if the primary provider fails, Theseus tries the next one automatically.

---

### Contacts

A contact book that the agent can use to look up email addresses and names. Supports CardDAV sync or a local JSON store.

**How to use:**
- Go to **Settings → Contacts** to configure CardDAV sync, or manage contacts directly under **Contacts** in the sidebar.
- The agent uses `resolve_contact` to look up contacts when composing emails.

---

### Vault

Integration with Bitwarden / Vaultwarden CLI for retrieving secrets. The agent can fetch passwords and API keys from your vault when needed.

**How to use:**
- Install the `bw` CLI on your server.
- Go to **Settings → Vault**, enter your server URL and email, and click **Unlock**.
- The session token is stored securely in `data/vault.json` (mode 0600).

---

### Shell

Admins can run shell commands directly from the Theseus UI, with streaming output. The agent also uses this for `bash` and `python` tool calls.

**Access:** Admin accounts only. Non-admin users cannot access the shell or run bash/python tools.

---

### TTS / STT

Text-to-speech and speech-to-text for voice interaction.

**TTS providers:** Disabled, browser (Web Speech API), OpenAI TTS, Kokoro (local).

**STT providers:** Disabled, browser (Web Speech API), local Whisper, OpenAI Whisper API.

**How to configure:** Go to **Settings → Voice**.

---

## Putting Theseus Behind HTTPS

Theseus serves plain HTTP. For any deployment accessible outside your local machine, put a TLS-terminating reverse proxy in front of it.

**With Caddy (auto-renews Let's Encrypt):**

```caddy
theseus.example.com {
  reverse_proxy localhost:7000
}
```

**With nginx:**

```nginx
server {
    listen 443 ssl;
    server_name theseus.example.com;
    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;
    location / {
        proxy_pass http://localhost:7000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;  # required for SSE streaming
    }
}
```

**Important:** Set `proxy_buffering off` (nginx) or equivalent — Theseus uses Server-Sent Events for streaming responses, which requires unbuffered proxying.

---

## Backup & Restore

**Export your data:**

Go to **Settings → Backup → Export**. This downloads a JSON file containing your memories, settings, feature flags, and preferences.

Or via the API:
```bash
curl -b "session_token=YOUR_TOKEN" http://localhost:7000/api/export -o backup.json
```

**Import a backup:**

Go to **Settings → Backup → Import** and upload your backup file. Existing data is preserved; the import merges new entries.

**What's included:** Memories, settings, features, preferences.

**What's not included:** Chat sessions, documents, gallery images, email accounts, calendar events. These live in `data/app.db` and `data/generated_images/`. Back up the entire `data/` directory for a full backup.

**Full backup (recommended):**
```bash
# Stop Theseus first, then:
cp -r data/ data_backup_$(date +%Y%m%d)/
```

---

## Updating

**Docker:**
```bash
docker compose pull
docker compose up -d --build
```

**Binary:**
1. Download the new binary from the Releases page.
2. Stop the running instance.
3. Replace the binary.
4. Start it again.

Database migrations run automatically on startup — no manual steps needed.

---

## Important Considerations

### Your data is yours

All data is stored in the `data/` directory on your server. Nothing is sent to external services unless you explicitly configure one (e.g. an OpenAI API key, a cloud email account). Even then, only the data you actively send to that service leaves your machine.

### Keep `data/` backed up

The `data/` directory contains your entire workspace: conversations, documents, memories, credentials, and the encryption key. Losing it means losing everything. Back it up regularly.

### Don't expose Theseus directly to the internet

Theseus is designed for trusted LAN or VPN use, or behind a reverse proxy with HTTPS. It has powerful capabilities (shell access, file I/O, email sending) that should not be exposed to untrusted networks without proper authentication and TLS.

### Admin accounts are powerful

Admin users can run shell commands, download and serve models, access all other users' data, and wipe the database. Create admin accounts only for people you fully trust. Regular users get a restricted set of capabilities controlled by per-user privileges.

### API tokens and webhooks

If you create API tokens or configure webhooks, treat them like passwords. Create separate tokens per integration, set the minimum required scopes, and delete unused ones. Webhook URLs must use `http` or `https` and cannot target loopback addresses.

### Two-factor authentication

Enable 2FA for your admin account under **Settings → Account → Two-Factor Authentication**. TOTP secrets are encrypted at rest.

### Model quality matters

The quality of agent behavior, research, and email drafts depends heavily on the model you use. Larger, more capable models produce better results. For agent tasks, a model with function-calling support (indicated by `supports_tools` on the endpoint) works best.

### Resource usage

Running local models requires significant hardware. The Cookbook feature helps you find models that fit your available VRAM and RAM. If you're using cloud APIs, be aware of token costs — the agent loop can make many LLM calls for complex tasks.
