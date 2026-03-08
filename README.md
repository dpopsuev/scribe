# Scribe

Lean governance artifact store with native DAG support. Manages contracts, specs, sprints, rules, goals, and their relationships via CLI or MCP server.

## Quickstart (container)

```bash
docker run -d --name scribe \
  -p 8080:8080 \
  -v scribe-data:/root/.scribe \
  quay.io/dpopsuev/scribe
```

## Quickstart (binary)

```bash
go install github.com/dpopsuev/scribe/cmd/scribe@latest
scribe serve                          # stdio (Cursor/Claude)
scribe serve --transport http         # HTTP on :8080
```

## Cursor MCP configuration

### stdio (local binary)

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

### HTTP (container)

```json
{
  "mcpServers": {
    "scribe": {
      "url": "http://localhost:8080/"
    }
  }
}
```

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `SCRIBE_DB` | `~/.scribe/scribe.sqlite` | Database path |
| `SCRIBE_TRANSPORT` | `stdio` | Transport: `stdio`, `http` |
| `SCRIBE_ADDR` | `:8080` | Listen address (http only) |

## MCP tools

| Tool | Description |
|---|---|
| `create_artifact` | Create a new governance artifact |
| `get_artifact` | Retrieve a single artifact by ID |
| `list_artifacts` | List with filters, grouping, sorting |
| `set_field` | Set a field on an artifact |
| `search_artifacts` | Substring search across title, goal, sections |
| `attach_section` | Add/replace a named text section |
| `get_section` | Retrieve a section's text |
| `contract_tree` | Parent-child tree view |
| `set_goal` | Set current goal, auto-create root artifact |
| `archive_artifact` | Archive with optional cascade |
| `vacuum` | Delete old archived artifacts |
| `motd` | Message of the day: goals, reminders, notes |
| `inventory` | Dashboard summary |
| `link_artifacts` | Add directed relationships |
| `unlink_artifacts` | Remove directed relationships |
| `drain_discover` | List legacy .md files for migration |
| `drain_cleanup` | Delete migrated .md files |
