# LLM Ingester

An HTTP proxy and Gravwell ingester for LLM traffic. Point a client at it
instead of the upstream provider and it transparently forwards traffic while
ingesting prompts, responses, tool calls, and token usage as structured
events. Two protocols are built in:

| `Protocol` | Endpoint | Clients |
| --- | --- | --- |
| `openai-chat` | `/v1/chat/completions` | OpenAI-compatible (Codex, opencode, crush, …) |
| `anthropic-messages` | `/v1/messages` | Claude Code, the Anthropic SDKs |

Requests on other paths are proxied through without being ingested.  Set
`Reject-Unknown-Paths` to lock a listener down to the one endpoint it parses.

See [`gravwell_llm_ingester.conf`](gravwell_llm_ingester.conf) and
[`config.go`](config.go) for all config options (log mode, auth, session
tracking, etc).

## Running the proxy

```shell
go build .
./llm_ingester -config-file gravwell_llm_ingester.conf
```

Point `Upstream-URL` in the config at your real provider (and set
`Upstream-Authorization` if you want the proxy to inject the real key instead
of passing the client's through).

## Claude Code

Claude Code talks to the Messages API (`POST /v1/messages`), so it needs an
`anthropic-messages` listener. There are two halves to configure: the listener
here, and Claude Code's base URL on the client.

### Listener

```ini
[Listener "anthropic"]
	Bind = ":4181"
	Upstream-URL = "https://api.anthropic.com"
	Protocol = "anthropic-messages"
	Tag-Name = llm

	# The Messages API authenticates with a bare "x-api-key" header rather
	# than "Authorization: Bearer".
	Auth-Style = "x-api-key"

	# Injected only when a request arrives without one.
	Anthropic-Version = "2023-06-01"

	# Claude Code stamps every request with its own conversation ID; adopt it
	# as the ingested session_id.
	Session-ID-Header = "x-claude-code-session-id"

	Log-Mode = "delta"
	Log-Tool-Calls = true
	Log-Usage = true
```

Start the ingester as above, and it is listening on `:4181`.

The antropic key can be held by claude code or the proxy:

| | Listener | Claude Code's `ANTHROPIC_API_KEY` |
| --- | --- | --- |
| Client's key passes through (default) | neither `Client-Authorization` nor `Upstream-Authorization` set | your real `sk-ant-…` key |
| Proxy holds the real key | `Upstream-Authorization = "sk-ant-…"`, and optionally `Client-Authorization = "<gate token>"` | the gate token, or any placeholder if `Client-Authorization` is unset |

Claude Code always wants *some* credential in its environment — it falls back
to a claude.ai login when it finds none — so in the second arrangement give it
the gate token rather than nothing.

### 2. Claude Code

Point it at the listener with `ANTHROPIC_BASE_URL`, either per-invocation:

```shell
ANTHROPIC_BASE_URL=http://localhost:4181 \
ANTHROPIC_API_KEY=$YOUR_ANTHROPIC_API_KEY \
  claude
```

or persistently in an `env` block in Claude Code's settings —
`.claude/settings.json` inside a project, or `~/.claude/settings.json` to
cover every project:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4181",
    "ANTHROPIC_API_KEY": "sk-ant-your-key-or-the-gate-token"
  }
}
```

Notes on the client side:

- No `/v1` on the base URL. Claude Code appends the full path itself
  (`POST /v1/messages?beta=true`).
- `ANTHROPIC_BASE_URL` covers every API call Claude Code makes, including the
  `/v1/messages/count_tokens` sibling the proxy passes through untouched, so
  nothing else needs configuring.
- `Auth-Style` has to match how Claude Code is credentialed:
  `ANTHROPIC_API_KEY` sends `x-api-key` (the block above), while
  `ANTHROPIC_AUTH_TOKEN` sends `Authorization: Bearer` and wants
  `Auth-Style = "bearer"` on the listener instead.
- Over plain HTTP, both the prompts and the key cross the wire in the clear.
  Set `TLS-Certificate-File` / `TLS-Key-File` on the listener and use an
  `https://` base URL for anything but a local proxy.

## OpenAI compatible clients

These clients talk to the Chat Completions API (`POST /v1/chat/completions`),
so they need an `openai-chat` listener. There are two halves to configure: the
listener here, and the client's base URL.

### Listener

```ini
[Listener "openai"]
	Bind = ":4180"
	Upstream-URL = "https://api.openai.com"
	Protocol = "openai-chat"
	Tag-Name = llm

	# Chat Completions authenticates with "Authorization: Bearer", which is
	# the default, so no Auth-Style is needed.

	Log-Mode = "delta"
	Log-Tool-Calls = true
	Log-Usage = true
```

Start the ingester as above, and it is listening on `:4180`.

The upstream key can be held by the client or the proxy:

| | Listener | Client's API key |
| --- | --- | --- |
| Client's key passes through (default) | neither `Client-Authorization` nor `Upstream-Authorization` set | your real upstream key |
| Proxy holds the real key | `Upstream-Authorization = "sk-…"`, and optionally `Client-Authorization = "<gate token>"` | the gate token, or any placeholder if `Client-Authorization` is unset |

Notes on the client side:

- The base URL *does* include `/v1` for these clients (`http://localhost:4180/v1`),
  unlike Claude Code's `ANTHROPIC_BASE_URL`.
- `Log-Usage` on a streaming client only produces a usage record if the client
  sets `stream_options.include_usage = true`; without it there is nothing in
  the stream to ingest.
- These clients send no conversation ID header, so sessions are derived by
  matching message prefixes. If your client does stamp one, name it with
  `Session-ID-Header` and it is adopted as the session ID instead.
- Over plain HTTP, both the prompts and the key cross the wire in the clear.
  Set `TLS-Certificate-File` / `TLS-Key-File` on the listener and use an
  `https://` base URL for anything but a local proxy.

### opencode

Add a custom provider pointing at the listener (default `:4180`) to
`opencode.json` / `opencode.jsonc`:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "llm-ingester": {
      "name": "LLM Ingester (local)",
      "options": {
        "baseURL": "http://localhost:4180/v1"
      },
      "models": {
        "<your-model>": {}
      }
    }
  }
}
```

### crush

Add a custom provider to `.crush.json` / `crush.json`:

```json
{
  "$schema": "https://charm.land/crush.json",
  "providers": {
    "llm-ingester": {
      "name": "LLM Ingester (local)",
      "type": "openai",
      "base_url": "http://localhost:4180/v1",
      "api_key": "$YOUR_API_KEY",
      "models": [
        { "id": "<your-model>", "name": "<your-model>" }
      ]
    }
  }
}
```

### Codex

See the [Codex proxy config docs](https://developers.openai.com/codex/config-advanced).

### curl

```shell
curl http://localhost:4180/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $YOUR_API_KEY" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}'

curl http://localhost:4181/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -H "x-api-key: $YOUR_ANTHROPIC_API_KEY" \
  -d '{"model":"claude-opus-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}'
```
