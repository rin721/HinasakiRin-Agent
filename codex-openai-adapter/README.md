# codex-openai-adapter

`codex-openai-adapter` is a local-only OpenAI-compatible adapter server for learning. It wraps the locally installed and already logged-in Codex CLI behind:

```text
POST /v1/chat/completions
GET /v1/models
```

It is not a production gateway. It lets a TypeScript coding agent call a local OpenAI-compatible endpoint while Codex handles text and image-input reasoning.

## Safety Model

- The adapter does not read, copy, extract, or commit Codex CLI / ChatGPT / OpenAI login tokens.
- The adapter does not access `~/.codex/auth.json`.
- `/v1/*` requests require `Authorization: Bearer local-api-token` by default.
- Prompts are sent to `codex exec` through stdin, not command-line arguments.
- Image inputs are written as temporary files under `./codex-workdir/attachments/` and removed after the request.
- Codex runs in `--sandbox read-only` with approval policy set to `never`.
- Codex runs inside `./codex-workdir`, not inside your code project.
- Codex is launched with `--ignore-user-config`, `--ignore-rules`, and `--skip-git-repo-check` so the adapter does not inherit project rules or broken local config. Codex auth still uses `CODEX_HOME`.
- On Windows, the adapter prefers `codex.cmd` over the PowerShell wrapper so CLI config arguments are preserved correctly.
- `.gitignore` excludes `codex-workdir/`, `.env`, and common auth/token file patterns.

The adapter adds a guard before every request: Codex should not directly inspect or modify local files from the adapter, but it may output JSON actions when the upstream agent asks for them.

## Supported API

- `POST /v1/chat/completions`: text chat plus image input through OpenAI `image_url` content parts.
- `GET /v1/models`: returns the Codex model catalog from `codex debug models`.
- `stream=true`: returns OpenAI-style SSE chunks using `codex exec --json`.

The CLI-only adapter does not fake unsupported capabilities. These endpoints return `unsupported_feature`:

```text
/v1/images/generations
/v1/images/edits
/v1/images/variations
/v1/audio/transcriptions
/v1/audio/speech
/v1/audio/translations
/v1/embeddings
```

Image input accepts only base64 data URLs such as `data:image/png;base64,...`. The adapter does not download remote image URLs and does not read arbitrary local image paths.

## Start

```bash
go mod tidy
go run ./cmd
```

By default the server listens on `http://localhost:8788`.

## Test

```bash
curl http://localhost:8788/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer local-api-token" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Return only JSON: {\"tool\":\"list_files\",\"args\":{}}"}],"temperature":0,"max_tokens":128}'
```

List models:

```bash
curl http://localhost:8788/v1/models \
  -H "Authorization: Bearer local-api-token"
```

Streaming chat:

```bash
curl http://localhost:8788/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer local-api-token" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Reply exactly: OK"}],"stream":true}'
```

## TypeScript Agent Config

Use this adapter from `mini-coding-agent` with:

```bash
MODEL_BASE_URL=http://localhost:8788/v1
MODEL_API_KEY=local-api-token
MODEL_NAME=auto
```

Then run the agent with its real model adapter.

## Config

Edit `config.yaml`:

```yaml
server:
  port: 8788

gateway:
  api_token: local-api-token

codex:
  safe_workdir: ./codex-workdir
  timeout_seconds: 120
  binary: codex
  default_model: ""
  service_tier: fast
  model_reasoning_effort: ""
  max_images: 10
  max_image_bytes: 20971520
```

`safe_workdir` is intentionally separate from your code project. The adapter creates it automatically if it does not exist.

`default_model` is used when the incoming request uses `"model": "auto"`. If it is empty, the adapter lets Codex CLI choose its default model.
