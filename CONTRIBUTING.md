# Contributing to Theseus

Thanks for your interest in contributing! Theseus is a Go port of the Odysseus self-hosted AI workspace. This guide covers everything you need to get started.

## Ways to Contribute

- **Report bugs** — open a [Bug Report](https://github.com/chaseputnam/theseus/issues/new?template=bug_report.yml)
- **Request features** — open a [Feature Request](https://github.com/chaseputnam/theseus/issues/new?template=feature_request.yml)
- **Submit pull requests** — bug fixes, new features, documentation improvements
- **Improve docs** — fix typos, clarify setup steps, add examples
- **Test on your hardware** — especially useful for Cookbook (model serving) and platform-specific issues

## Development Setup

### Prerequisites

- Go 1.22 or later
- Docker and Docker Compose (optional, for running ChromaDB + SearXNG locally)
- `tmux` (optional, required for Cookbook model serving)

### Getting Started

```bash
git clone https://github.com/chaseputnam/theseus.git
cd theseus

# Download dependencies
go mod download

# Build the binary
go build -o theseus ./cmd/theseus

# Run tests
go test ./...

# Start the server (auth disabled for local dev)
./theseus --port 7000 --data-dir ./data --static-dir ./static
```

Open `http://localhost:7000`. On first boot, an admin password is printed to the terminal.

### Running with Docker (recommended for full feature testing)

```bash
docker compose up -d --build
docker compose logs --tail=50 theseus
```

This starts Theseus alongside ChromaDB (vector memory), SearXNG (web search), and ntfy (notifications).

### Project Structure

```
cmd/theseus/          Entry point — flag parsing, server init
internal/
  auth/               Authentication, session tokens, TOTP, privileges
  db/                 SQLite schema, migrations, query helpers
  storage/            AES-256-GCM encryption, atomic file I/O
  settings/           Settings/features JSON with TTL cache
  llm/                OpenAI-compatible streaming HTTP client
  agent/              Agent loop, fenced-block parser, SSE writer
  tools/              Built-in tool implementations
  memory/             Memory manager, ChromaDB client, RAG, skills
  search/             Multi-provider web search
  mcp/                MCP protocol client (stdio + SSE)
  research/           Deep research engine, HTML report generator
  calendar/           Calendar CRUD, CalDAV sync, iCal import/export
  email/              IMAP client, SMTP client
  cookbook/           Hardware detection, model catalog, tmux serving
  tts/                TTS/STT provider abstraction
  webhook/            Outgoing webhook delivery, HMAC signing
  server/             HTTP route handlers, middleware, SSE helper
static/               Frontend HTML/CSS/JS (from Odysseus — do not modify)
specs/                Spec documents (requirements, design, tasks, review)
```

### Build Commands

```bash
# Build for local platform
go build -o theseus ./cmd/theseus

# Run all tests
go test ./...

# Run tests with race detection (recommended before submitting a PR)
go test -race -timeout 120s ./...

# Run vet
go vet ./...

# Tidy dependencies
go mod tidy

# Cross-compile (example: Linux arm64)
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o theseus-linux-arm64 ./cmd/theseus
```

## Making Changes

1. **Fork** the repository and create a branch from `main`:
   ```bash
   git checkout -b fix/describe-your-change
   ```

2. **Make your changes.** Keep commits focused — one logical change per commit.

3. **Run tests and vet** before pushing:
   ```bash
   go vet ./...
   go test -race -timeout 120s ./...
   go mod tidy
   git diff --exit-code go.mod go.sum
   ```

4. **Open a pull request** against `main`. Fill out the PR template — describe what changed, how you tested it, and any tradeoffs you considered.

## Branch Naming

| Type | Pattern | Example |
|---|---|---|
| Bug fix | `fix/short-description` | `fix/imap-tls-config` |
| Feature | `feat/short-description` | `feat/caldav-push-sync` |
| Docs | `docs/short-description` | `docs/windows-setup` |
| Chore | `chore/short-description` | `chore/bump-go-imap` |
| Security | `security/short-description` | `security/ssrf-private-ip-block` |

## Code Conventions

- Follow standard Go formatting — `gofmt` is enforced by CI.
- Keep package responsibilities focused. Don't reach across package boundaries without a good reason — see `ARCHITECTURE.md` for the intended separation.
- Add or update tests for any logic changes. Test files live alongside source files (`*_test.go`).
- Avoid adding new dependencies unless necessary. Discuss in an issue first. All dependencies must be pinned in `go.sum`.
- Do not commit large binary files or generated data.
- The `static/` directory is the Odysseus frontend and is not modified in this repo. Frontend changes belong upstream.
- Error returns from database writes must be checked and logged — do not silently discard them.
- Secrets (passwords, tokens, API keys) must never be logged or returned in API responses in plaintext.

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) style:

```
feat: add per-user allowed_models privilege enforcement
fix: use UIDExpunge to avoid expunging unrelated deleted messages
docs: add CalDAV sync troubleshooting section
chore: bump go-imap to v2.0.0-beta.6
test: add path traversal tests for safePath
security: block RFC 1918 addresses in webhook URL validation
```

## Testing Guidelines

- Write tests for any new logic, especially security-sensitive paths (auth, encryption, file access, privilege checks).
- Use `t.TempDir()` for tests that need filesystem access — it cleans up automatically.
- Use `httptest.NewServer` and `httptest.NewRecorder` for HTTP handler tests.
- Run `go test -race` before submitting — the race detector catches concurrency bugs that normal tests miss.
- Tests that require external services (ChromaDB, IMAP, CalDAV) should be skipped when those services are unavailable, not fail.

## Pull Request Checklist

Before marking your PR ready for review:

- [ ] `go vet ./...` passes
- [ ] `go test -race -timeout 120s ./...` passes
- [ ] `go mod tidy` produces no diff
- [ ] New functionality has tests
- [ ] Security-sensitive changes are noted in the PR description
- [ ] Documentation is updated if behavior changes

## Reporting Security Issues

Do **not** open a public issue for security vulnerabilities. See [SECURITY.md](SECURITY.md) for the responsible disclosure process.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
