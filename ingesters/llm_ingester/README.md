# LLM Ingester

An OpenAI-compatible HTTP proxy and Gravwell ingester. Point a client at it
instead of the upstream provider and it transparently forwards traffic while
ingesting prompts, responses, tool calls, and token usage as structured
events. See [`gravwell_llm_ingester.conf`](gravwell_llm_ingester.conf) and
[`config.go`](config.go) for all config options (log mode, auth, session
tracking, etc).

## Local testing

\`\`\`markdown
go build .
./llm_ingester -config-file gravwell_llm_ingester.conf
\`\`\`

Point `Upstream-URL` in the config at your real provider (and set
`Upstream-Authorization` if you want the proxy to inject the real key instead
of passing the client's through).

### opencode

Add a custom provider pointing at the listener (default `:4180`) to
`opencode.json` / `opencode.jsonc`:

`\`\`jsonc
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
\`\`\`

### crush

Add a custom provider to `.crush.json` / `crush.json`:

\`\`\`json
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
\`\`\`

### Codex / Claude Code

See their proxy config docs:

- [Codex](https://developers.openai.com/codex/config-advanced)
- [Claude Code](https://code.claude.com/docs/en/network-config)

### curl

\`\`\`shell
curl http://localhost:4180/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $YOUR_API_KEY" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}'
\`\`\`

Search the configured tag (`llm` by default) in Gravwell to see ingested
events.

Run tests with `go test ./...`.
