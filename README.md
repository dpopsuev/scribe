<p align="center">
  <img src="assets/scribe_logo.png" alt="Scribe" width="200">
</p>

# Scribe

Work graph for AI agents. Scribe is a structured artifact store with native DAG support that lets AI coding assistants plan, track, and recall work across sessions -- beyond the limits of a single context window.

## Quick Start

### Container (recommended)

```bash
# podman or docker
podman run -d --name scribe \
  -p 8080:8080 \
  -v scribe-data:/data \
  quay.io/dpopsuev/scribe:latest
```

### Binary

```bash
go install github.com/dpopsuev/scribe/cmd/scribe@latest
scribe serve                   # stdio (Cursor, Claude Desktop)
scribe serve --transport http  # Streamable HTTP on :8080
```

### MCP Configuration

**Cursor / Claude Desktop (stdio -- local binary):**

```json
{
  "mcpServers": {
    "scribe": {
      "command": "scribe",
      "args": ["serve"]
    }
  }
}
```

**Cursor / Claude Desktop (HTTP -- container):**

```json
{
  "mcpServers": {
    "scribe": {
      "url": "http://localhost:8080/?workspace=origami"
    }
  }
}
```

## Workflow

Scribe is designed for natural language. You talk to your agent, and it builds the work graph behind the scenes.

> **You:** Plan a sprint for authentication and rate limiting. Write specs first.
>
> **Agent:** *(creates 2 specs, 1 sprint, 3 tasks, links them)* Done. I created specs for "OAuth2 authentication" and "API rate limiting", a sprint called "Sprint 1: Foundation", and 3 tasks implementing those specs. The rate-limiting task depends on auth being done first. Here's the board...

> **You:** I finished the auth task. What's next?
>
> **Agent:** *(marks task complete, checks dependencies)* Auth is done. The rate-limiting task is now unblocked — it was waiting on auth. I've marked it active. The sprint has 1 of 3 tasks complete.

> **You:** We found a token leak bug. Track it.
>
> **Agent:** *(creates bug, creates task implementing it, adds to sprint)* Filed `BUG-2026-001: Auth token leak`. Created a task to fix it and added it to the current sprint. The sprint now has 4 tasks: 1 complete, 1 active, 2 draft.

> **You:** Show me the sprint board.
>
> **Agent:** *(calls tree on the sprint)* Here's the full tree with status, dependencies, and which specs/bugs each task implements. You're 25% through the sprint.

## The Problem

LLM context windows are finite. A coding agent can hold ~100K tokens in working memory. When a session ends, everything it learned -- goals, decisions, dependencies, progress -- evaporates.

This creates three failure modes:

1. **Amnesia.** The agent re-discovers the same codebase from scratch every session.
2. **Drift.** Multi-session work loses coherence because there's no shared record of what was decided and why.
3. **Fragmentation.** Plans scattered across chat logs, markdown files, and issue trackers can't be queried or traversed as a graph.

Scribe solves this by giving agents a structured, persistent memory they can read and write through MCP tools -- a place to store goals, specs, tasks, bugs, sprints, and their relationships in a queryable DAG.

## Core Concepts

| Concept | What it is |
|---|---|
| **Artifact** | The universal record. Everything is an artifact with a kind, status, scope, and auto-generated ID (e.g. `TASK-2026-042`). |
| **Kind** | The type of artifact. Canonical kinds: `goal`, `sprint`, `task`, `spec`, `bug`, `campaign`, `template`, `need`, `ref`, `doc`, `decision`, `config`. Enforced by schema validation. |
| **Task** | The primary unit of work. A task carries a goal statement, design sections, and dependency edges. Tasks **implement** specs and bugs. |
| **Spec** | A specification: the *what* and *why*. Defines acceptance criteria. Tasks implement specs. |
| **Bug** | A defect record. Like a spec, a bug is resolved by a task that implements it. |
| **Goal** | The north-star artifact for a scope. Setting a goal auto-creates a root delivery artifact and archives any previous goal. |
| **Status** | Lifecycle state: `draft` &rarr; `active` &rarr; `complete` / `dismissed`. Also: `current` (goals), `retired`, `archived`. |
| **Scope** | The project or repository an artifact belongs to (e.g. `locus`, `origami`). Enables multi-project planning from a single Scribe instance. |
| **Section** | A named text block attached to an artifact. Use for design notes, mermaid diagrams, acceptance criteria, or any structured content. |
| **Campaign** | Cross-project mission container. Groups goals, specs, and tasks under a theme (e.g. "v1 stabilization"). |
| **Template** | Meta-artifact (artifact about artifacts). Defines required sections and guidance for a kind. Artifacts link to templates via `satisfies`. |
| **Need** | A requirement or capability gap. Justifies goals and specs. |
| **Config** | Runtime configuration artifact. Sections are key-value pairs with cascading resolution (scoped > global). |
| **Edge** | A directed relationship: `parent_of`, `depends_on`, `follows`, `justifies`, `implements`, `documents`, `satisfies`. Edges form a DAG that agents can traverse. |

### Artifact Relationships

```mermaid
graph LR
    subgraph "Defines Work"
        SPEC["spec"]
        BUG["bug"]
    end

    subgraph "Does Work"
        TASK["task"]
    end

    subgraph "Organizes Work"
        GOAL["goal"]
        SPRINT["sprint"]
        CAMPAIGN["campaign"]
    end

    subgraph "Guides Work"
        TEMPLATE["template"]
    end

    TASK -- implements --> SPEC
    TASK -- implements --> BUG
    TASK -- depends_on --> TASK
    TASK -- satisfies --> TEMPLATE
    SPEC -- satisfies --> TEMPLATE
    SPRINT -- parent_of --> TASK
    GOAL -- parent_of --> SPEC
    GOAL -- parent_of --> TASK
    GOAL -- parent_of --> BUG
    GOAL -. justifies .-> GOAL
```

**Specs** and **bugs** define *what* needs to happen. **Tasks** do the work by implementing specs or resolving bugs. **Sprints** group tasks into time-boxed iterations. **Goals** sit at the top as north-star containers. The `detect` admin tool warns when a task has no spec/bug link, or when a spec/bug has no task implementing it.

### Example Artifact Graph

```mermaid
graph TD
    GOAL-2026-001["GOAL-2026-001\nShip v1.0\n(current)"]

    GOAL-2026-002["GOAL-2026-002\nv1.0 Delivery\n(active)"]
    GOAL-2026-002 -.->|justifies| GOAL-2026-001

    SPR-2026-001["SPR-2026-001\nSprint 1: Foundation\n(active)"]

    SPE-2026-001["SPE-2026-001\nAuth spec\n(draft)"]
    SPE-2026-001 -->|parent_of| GOAL-2026-002

    TASK-2026-001["TASK-2026-001\nAdd authentication\n(complete)"]
    TASK-2026-001 -->|parent_of| SPR-2026-001
    TASK-2026-001 -.->|implements| SPE-2026-001

    TASK-2026-002["TASK-2026-002\nAdd rate limiting\n(active)"]
    TASK-2026-002 -->|parent_of| SPR-2026-001
    TASK-2026-002 -.->|depends_on| TASK-2026-001

    BUG-2026-001["BUG-2026-001\nAuth token leak\n(draft)"]

    TASK-2026-003["TASK-2026-003\nFix token leak\n(draft)"]
    TASK-2026-003 -->|parent_of| SPR-2026-001
    TASK-2026-003 -.->|implements| BUG-2026-001
```

Solid arrows are `parent_of` edges (tree structure). Dashed arrows are `implements`, `depends_on`, or `justifies` edges (semantics). The agent walks this graph to find what to work on next: the highest-priority unblocked task whose dependencies are all complete.

## Architecture

> Diagrams generated by [Locus](https://github.com/dpopsuev/locus) (`locus diagram --theme natural`). Components colored by health: green = healthy, yellow = sick (fan-in >= 3, churn >= 8), red = fatal.

### Dependency Graph

> `locus diagram /path/to/scribe --type dependency --theme natural`

```mermaid
%%{init: {'theme': 'base', 'themeVariables': {'primaryColor': '#F7FAFC', 'primaryTextColor': '#2D3748', 'primaryBorderColor': '#4A90D9', 'lineColor': '#4A90D9', 'background': '#FFFFFF', 'fontSize': '14px'}}}%%
graph TD
    classDef boundary fill:#FFFFFF,stroke:#718096,color:#2D3748
    classDef component fill:#F7FAFC,stroke:#4A90D9,color:#2D3748
    classDef edge stroke:#4A90D9
    classDef entry fill:#4A90D9,stroke:#4A90D9,color:#FFFFFF
    classDef fatal fill:#E53E3E,stroke:#E53E3E,color:#FFFFFF
    classDef healthy fill:#38A169,stroke:#38A169,color:#FFFFFF
    classDef sick fill:#D69E2E,stroke:#D69E2E,color:#FFFFFF
    classDef violation_edge stroke:#E53E3E
    cmd_scribe["cmd/scribe [churn:12]"]:::entry
    config["config [churn:6]"]:::healthy
    directive["directive [churn:6]"]:::healthy
    keygen["keygen [churn:2]"]:::healthy
    lifecycle["lifecycle [churn:7]"]:::healthy
    mcp["mcp [churn:20]"]:::healthy
    mcpclient["mcpclient [churn:1]"]:::healthy
    model["model [churn:9]"]:::sick
    protocol["protocol [churn:14]"]:::sick
    render["render [churn:1]"]:::healthy
    store["store [churn:13]"]:::sick
    web["web [churn:6]"]:::healthy
    cmd_scribe -->|"13"| config
    cmd_scribe -->|"7"| directive
    cmd_scribe -->|"2"| mcp
    cmd_scribe -->|"6"| mcpclient
    cmd_scribe -->|"18"| model
    cmd_scribe -->|"90"| protocol
    cmd_scribe -->|"3"| render
    cmd_scribe -->|"6"| store
    cmd_scribe -->|"1"| web
    config -->|"2"| model
    config -->|"5"| protocol
    config -->|"1"| store
    lifecycle -->|"6"| model
    lifecycle -->|"6"| store
    mcp -->|"8"| directive
    mcp -->|"6"| mcpclient
    mcp -->|"9"| model
    mcp -->|"88"| protocol
    mcp -->|"3"| render
    mcp -->|"1"| store
    protocol -->|"1"| keygen
    protocol -->|"5"| lifecycle
    protocol -->|"57"| model
    protocol -->|"21"| store
    render -->|"24"| model
    store -->|"40"| model
    web -->|"2"| model
    web -->|"12"| protocol
```

### Layer Diagram

> `locus diagram /path/to/scribe --type layers --theme natural`

```mermaid
%%{init: {'theme': 'base', 'themeVariables': {'primaryColor': '#F7FAFC', 'primaryTextColor': '#2D3748', 'primaryBorderColor': '#4A90D9', 'lineColor': '#4A90D9', 'background': '#FFFFFF', 'fontSize': '14px'}}}%%
block-beta
    columns 1
    block:layer_0["Layer 0"]
        cmd_scribe["cmd/scribe"]
    end
    columns 3
    block:layer_1["Layer 1"]
        config["config"]
        mcp["mcp"]
        web["web"]
    end
    columns 4
    block:layer_2["Layer 2"]
        directive["directive"]
        mcpclient["mcpclient"]
        protocol["protocol"]
        render["render"]
    end
    columns 2
    block:layer_3["Layer 3"]
        keygen["keygen"]
        lifecycle["lifecycle"]
    end
    columns 1
    block:layer_4["Layer 4"]
        store["store"]
    end
    columns 1
    block:layer_5["Layer 5"]
        model["model"]
    end
```

### Packages

| Package | Churn | Role |
|---|---|---|
| `cmd/scribe` | 12 | CLI entry point. Every MCP tool has a CLI equivalent. |
| `mcp` | 20 | MCP server. Thin handlers that delegate to `protocol`. |
| `protocol` | 14 | All business logic. Both CLI and MCP are wrappers around this. |
| `model` | 9 | Data model: `Artifact`, `Section`, `Edge`, `Filter`, `Schema`. |
| `store` | 13 | Persistence interface + SQLite implementation. |
| `render` | 1 | Markdown and table formatters for CLI and MCP output. |
| `config` | 6 | Configuration loading, workspace definitions, schema. |
| `directive` | 6 | MCP tool registry and input validation. |
| `keygen` | 2 | Auto-generated ID sequences per artifact kind. |
| `web` | 6 | Web UI for artifact browsing and sprint boards. |

### Storage

Single SQLite database (CGo-free via `modernc.org/sqlite`). Tables:

- **artifacts** -- UID primary key (crypto/rand hex), human-readable ID as unique index with auto-rename on collision. JSON for arrays/maps.
- **edges** -- directed graph: `(from, to, relation)` with a unique constraint.
- **sequences** / **scoped_sequences** -- auto-increment counters with collision checks.

Snapshots: automatic periodic backups with pluggable backend (local filesystem, future S3). Restore with pre-restore backup via `admin snapshot restore`.

Default location: `~/.scribe/scribe.sqlite` (binary) or `/data/scribe.sqlite` (container).

### Data Model

Every artifact carries:

- **Identity:** auto-generated ID (`PREFIX-YYYY-SEQ`), kind, scope
- **Content:** title, goal statement, named sections (arbitrary text blocks)
- **Graph:** parent, depends_on edges, typed links (justifies, implements, documents)
- **Lifecycle:** status, priority, sprint assignment, labels, timestamps
- **Extension:** `extra` map for domain-specific key-value pairs (reminders, custom fields)

The vocabulary is enforced: unknown kinds are rejected with a hint to register them via `scribe vocab add`. Unknown fields go into `extra`.

## MCP Tools

| Tool | Description |
|---|---|
| `artifact` | Create, read, update, and manage work artifacts. Actions: `create`, `batch_create`, `clone`, `get`, `list`, `set`, `update`, `archive`, `attach_section`, `get_section`, `detach_section`. Supports compact list (custom fields including depends_on, labels), count mode with group_by, and template auto-linking. |
| `graph` | Navigate and modify artifact relationships. Actions: `tree` (hierarchy), `briefing` (full context chain), `topo_sort` (execution order by depends_on), `link` (add edge), `unlink` (remove edge). Relations: parent_of, depends_on, follows, justifies, implements, documents, satisfies. |
| `admin` | System administration and monitoring. Actions: `motd` (session context with goals/reminders), `changelog` (recent changes), `dashboard` (health/staleness), `snapshot` (create/list/diff/restore), `set_goal` (north star), `vacuum` (cleanup), `detect` (orphans/overlaps), `lint` (schema consistency). |

## Configuration

Scribe works with zero configuration. For customization, create a `scribe.yaml`:

```yaml
# scribe.yaml
db: ~/.scribe/scribe.sqlite
transport: stdio
addr: ":8080"

scopes:
  - myproject

vocabulary:
  kinds:
    - goal
    - sprint
    - task
    - spec
    - bug
    # add your own via: scribe vocab add <kind>

schema:
  kinds:
    goal:          { prefix: GOAL }
    sprint:        { prefix: SPR }
    task:          { prefix: TASK }
    spec:          { prefix: SPE }
    bug:           { prefix: BUG }
    campaign:      { prefix: CON }
    template:      { prefix: TPL }
    need:          { prefix: NED }

  statuses:
    - draft
    - active
    - current
    - complete
    - dismissed
    - archived

  guards:
    archived_readonly: true
    completion_requires_children_complete: true
    auto_archive_goal_on_justify_complete: true
    delete_requires_archived: true
    auto_complete_parent_on_children_terminal: true
    auto_activate_next_draft_sprint: true

workspaces:
  origami: [origami, asterisk, achilles]
  sidecar: [scribe, locus, lex, limes]
```

**Resolution order:** `--config` flag > `$SCRIBE_CONFIG` > `./scribe.yaml` > `$SCRIBE_ROOT/scribe.yaml` > `~/.scribe/scribe.yaml` > built-in defaults.

**Override chain:** CLI flags > environment variables > config file > defaults.

**Workspace connections (HTTP):** Add `?workspace=origami` to the MCP URL to scope a connection to specific scopes.

For containers, mount a config file at `/data/scribe.yaml`:

```bash
podman run -d --name scribe \
  -p 8080:8080 \
  -v scribe-data:/data \
  -v ./scribe.yaml:/data/scribe.yaml \
  quay.io/dpopsuev/scribe:latest
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `SCRIBE_ROOT` | `~/.scribe` | Storage root; sets default DB and config paths |
| `SCRIBE_DB` | `$SCRIBE_ROOT/scribe.sqlite` | Database path (overrides `SCRIBE_ROOT`) |
| `SCRIBE_TRANSPORT` | `stdio` | Transport: `stdio` or `http` |
| `SCRIBE_ADDR` | `:8080` | Listen address (HTTP transport only) |
| `SCRIBE_CONFIG` | `./scribe.yaml` or `$SCRIBE_ROOT/scribe.yaml` | Path to config file (first found wins) |
| `SCRIBE_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error`. JSON to stderr. |
| `SCRIBE_WORKSPACE` | — | Workspace name for stdio transport (resolves scopes from config). |

## License

MIT
