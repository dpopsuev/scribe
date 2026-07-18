# @danypops/pi-scribe

Native Pi extension for [Scribe](https://github.com/dpopsuev/scribe) — the typed graph store for AI agents.

## What it gives the agent

- **`scribe_artifact`** — create, get, query, set, update, delete artifacts on the work graph
- **`scribe_graph`** — link/unlink edges, analyze (fan-in/out, pagerank, paths), synonyms
- **`scribe_admin`** — dashboard, triage, hygiene, history, changelog, status

Plus a **session-start widget** (work-graph status above the editor) and **`/scribe`** (interactive graph browser).

## How it works

The extension talks plain REST to Scribe's `/api/v1/ops` facade — the same `service.Registry` that MCP and CLI dispatch through. Zero per-verb client logic; new scribe ops appear automatically.

## Install

```bash
pi install git:github.com/dpopsuev/scribe
# or: pi install npm:@danypops/pi-scribe
```

## Env

- `SCRIBE_URL` — scribe service URL (default `http://127.0.0.1:8080`)
- `SCRIBE_AUTH_TOKEN` — bearer token if scribe has auth enabled
- `SCRIBE_TIMEOUT_MS` — request timeout (default 15000)
