# Scribe

Persistent planning memory for AI agents. Scribe is a structured artifact store that lets AI coding assistants plan, track, and recall work across sessions -- beyond the limits of a single context window.

## Quick Start

### Container (recommended)

```bash
# podman or docker
podman run -d --name scribe \
  -p 8080:8080 \
  -v scribe-data:/data \
  quay.io/dpopsuev/scribe:0.1.1
```

### Binary

```bash
go install github.com/dpopsuev/scribe/cmd/scribe@v0.1.1
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
      "url": "http://localhost:8080/"
    }
  }
}
```

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
| **Kind** | The type of artifact. Canonical kinds: `goal`, `sprint`, `task`, `spec`, `bug`. Enforced by vocabulary validation -- unknown kinds are rejected with a hint to register them via `scribe vocab add`. |
| **Task** | The primary unit of work. A task carries a goal statement, design sections, and dependency edges. Tasks **implement** specs and bugs. |
| **Spec** | A specification: the *what* and *why*. Defines acceptance criteria. Tasks implement specs. |
| **Bug** | A defect record. Like a spec, a bug is resolved by a task that implements it. |
| **Goal** | The north-star artifact for a scope. Setting a goal auto-creates a root delivery artifact and archives any previous goal. |
| **Sprint** | A time-boxed container. Child tasks are the work items. Tree views show progress at a glance. |
| **Status** | Lifecycle state: `draft` &rarr; `active` &rarr; `complete` / `dismissed`. Also: `current` (goals), `retired`, `archived`. |
| **Scope** | The project or repository an artifact belongs to (e.g. `locus`, `origami`). Enables multi-project planning from a single Scribe instance. |
| **Section** | A named text block attached to an artifact. Use for design notes, mermaid diagrams, acceptance criteria, or any structured content. |
| **Edge** | A directed relationship: `parent_of`, `depends_on`, `justifies`, `implements`, `documents`, `satisfies`. Edges form a DAG that agents can traverse. |

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
    end

    TASK -- implements --> SPEC
    TASK -- implements --> BUG
    TASK -- depends_on --> TASK
    SPRINT -- parent_of --> TASK
    GOAL -- parent_of --> SPEC
    GOAL -- parent_of --> TASK
    GOAL -- parent_of --> BUG
    GOAL -. justifies .-> GOAL
```

**Specs** and **bugs** define *what* needs to happen. **Tasks** do the work by implementing specs or resolving bugs. **Sprints** group tasks into time-boxed iterations. **Goals** sit at the top as north-star containers. The `detect_orphans` tool warns when a task has no spec/bug link, or when a spec/bug has no task implementing it.

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

## Workflow

In the intended mode of operation, **the agent does all of this for you**. You describe what you want in natural language -- "plan a sprint for auth and rate limiting" -- and the agent calls the Scribe MCP tools to create goals, sprints, tasks, specs, sections, and status updates on your behalf. The CLI examples below show what's happening under the hood; you shouldn't need to run them manually.

### 1. Set a goal

The agent (or you) sets the north star for a project scope:

```bash
scribe goal set "Ship v1.0 with full MCP coverage" --scope myproject
```

This creates a `GOAL` artifact (status: `current`) and a root delivery artifact linked via `justifies`.

### 2. Define specs, plan work

Create specs (what needs to happen), then tasks (who does it), grouped into sprints:

```bash
scribe create --kind spec --title "Authentication system" --scope myproject
scribe create --kind sprint --title "Sprint 1: Foundation" --scope myproject
scribe create --kind task --title "Add authentication" --parent SPR-2026-001 --scope myproject
scribe link TASK-2026-001 implements SPE-2026-001
```

Attach design details as sections:

```bash
scribe section add TASK-2026-001 design "JWT-based auth with refresh tokens."
scribe section add SPE-2026-001 acceptance "All endpoints require valid JWT. Refresh within 5min window."
```

### 3. Execute

As agents work through tasks, they update status:

```bash
scribe set TASK-2026-001 status active    # starting work
scribe set TASK-2026-001 status complete  # done
```

Guards enforce consistency:
- A sprint can't be completed if it has non-complete children.
- When all children of a parent are terminal, the parent auto-completes.
- When the root delivery artifact completes, the goal auto-archives.
- Archived artifacts are read-only.

### 4. Resume

Every new session starts with `motd` (message of the day) to restore context:

```bash
scribe motd
```

Returns current goals, due reminders, and recent notes -- enough for an agent to pick up where it left off without re-reading the entire history.

### 5. Query

Search, filter, and traverse the artifact graph:

```bash
scribe list --kind contract --status active --scope myproject
scribe search "authentication"
scribe tree SPR-2026-001           # sprint board as a tree
scribe inventory                    # dashboard: counts by kind, status, active sprints
```

## Architecture

> Diagrams generated by [Locus](https://github.com/dpopsuev/locus) (`locus diagram`).

### Layer Diagram

```mermaid
block-beta
    columns 1
    block:layer_0["Layer 0 — Entry"]
        cmd_scribe["cmd/scribe"]
    end
    columns 1
    block:layer_1["Layer 1 — Transport"]
        mcp["mcp"]
    end
    columns 3
    block:layer_2["Layer 2 — Services"]
        mcpclient["mcpclient"]
        protocol["protocol"]
        render["render"]
    end
    columns 1
    block:layer_3["Layer 3 — Rules"]
        lifecycle["lifecycle"]
    end
    columns 1
    block:layer_4["Layer 4 — Persistence"]
        store["store"]
    end
    columns 1
    block:layer_5["Layer 5 — Data"]
        model["model"]
    end
```

### Dependency Graph

```mermaid
graph TD
    cmd_scribe["cmd/scribe"]
    lifecycle["lifecycle"]
    mcp["mcp"]
    mcpclient["mcpclient"]
    model["model"]
    protocol["protocol"]
    render["render"]
    store["store"]
    cmd_scribe --> mcp
    cmd_scribe --> mcpclient
    cmd_scribe --> model
    cmd_scribe --> protocol
    cmd_scribe --> render
    cmd_scribe --> store
    lifecycle --> model
    lifecycle --> store
    mcp --> mcpclient
    mcp --> model
    mcp --> protocol
    mcp --> render
    mcp --> store
    protocol --> lifecycle
    protocol --> model
    protocol --> store
    render --> model
    store --> model
```

### Packages

| Package | Role |
|---|---|
| `cmd/scribe` | CLI entry point. Every MCP tool has a CLI equivalent. |
| `mcp` | MCP server. Thin handlers that delegate to `protocol`. |
| `protocol` | All business logic. Both CLI and MCP are wrappers around this. |
| `model` | Data model: `Artifact`, `Section`, `Edge`, `Filter`, `Schema`. |
| `store` | Persistence interface + SQLite implementation. |
| `lifecycle` | Guards (archived=readonly, delete-requires-archived), archive with cascade, vacuum. |
| `render` | Markdown and table formatters for CLI and MCP output. |
| `mcpclient` | Optional client for cross-tool communication (e.g. querying Locus). |

### Storage

Single SQLite database (CGo-free via `modernc.org/sqlite`). Three tables:

- **artifacts** -- all fields as columns, JSON for arrays/maps (sections, labels, depends_on, links, extra).
- **edges** -- directed graph: `(from, to, relation)` with a unique constraint.
- **sequences** -- auto-increment counters per ID prefix (CON, SPR, GOAL, ...).

Default location: `~/.scribe/scribe.sqlite` (binary) or `/data/scribe.sqlite` (container).

### Data Model

Every artifact carries:

- **Identity:** auto-generated ID (`PREFIX-YYYY-SEQ`), kind, scope
- **Content:** title, goal statement, named sections (arbitrary text blocks)
- **Graph:** parent, depends_on edges, typed links (justifies, implements, documents, satisfies)
- **Lifecycle:** status, priority, sprint assignment, labels, timestamps
- **Extension:** `extra` map for domain-specific key-value pairs (reminders, custom fields)

The vocabulary is enforced: unknown kinds are rejected with a hint to register them via `scribe vocab add`. Unknown fields go into `extra`.

## MCP Tools

| Tool | Description |
|---|---|
| `motd` | Message of the day: current goals, due reminders, recent notes. Start here. |
| `create_artifact` | Create a new artifact (task, spec, goal, sprint, bug, decision). |
| `get_artifact` | Retrieve a single artifact by ID with all sections and metadata. |
| `list_artifacts` | List/search with filters (kind, scope, status, parent, sprint, id_prefix, exclude_kind, exclude_status, query), grouping, sorting, limits. |
| `set_field` | Set any field on an artifact (status, title, parent, sprint, labels, etc.). |
| `set_goal` | Set the north-star goal for a scope. Archives previous goal, creates root delivery artifact. |
| `attach_section` | Add or replace a named text section on an artifact. |
| `get_section` | Retrieve a section's text by name. |
| `contract_tree` | Render the parent-child tree rooted at any artifact. |
| `link_artifacts` | Add/remove directed relationships. Set `unlink=true` to remove. |
| `archive_artifact` | Archive by IDs (cascade supported) or by filter predicates (scope, kind, status, id_prefix). Supports dry_run. |
| `vacuum` | Delete archived artifacts older than N days. Scoped via `--scope`, protected kinds require `--force`. |
| `dashboard` | Housekeeping dashboard: per-scope counts, staleness, DB size, top stale artifacts. |
| `detect_orphans` | Find orphans and/or overlaps. Use `check=orphans\|overlaps\|all` (default: all). |

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
    note:          { prefix: NOTE, exclude_from_list: true }

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
```

**Resolution order:** `--config` flag > `$SCRIBE_CONFIG` > `./scribe.yaml` > `$SCRIBE_ROOT/scribe.yaml` > `~/.scribe/scribe.yaml` > built-in defaults.

**Override chain:** CLI flags > environment variables > config file > defaults.

For containers, mount a config file at `/data/scribe.yaml`:

```bash
podman run -d --name scribe \
  -p 8080:8080 \
  -v scribe-data:/data \
  -v ./scribe.yaml:/data/scribe.yaml \
  quay.io/dpopsuev/scribe:0.1.1
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `SCRIBE_ROOT` | `~/.scribe` | Storage root; sets default DB and config paths |
| `SCRIBE_DB` | `$SCRIBE_ROOT/scribe.sqlite` | Database path (overrides `SCRIBE_ROOT`) |
| `SCRIBE_TRANSPORT` | `stdio` | Transport: `stdio` or `http` |
| `SCRIBE_ADDR` | `:8080` | Listen address (HTTP transport only) |
| `SCRIBE_CONFIG` | `./scribe.yaml` or `$SCRIBE_ROOT/scribe.yaml` | Path to config file (first found wins) |

## License

MIT
