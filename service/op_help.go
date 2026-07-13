//nolint:goconst // help topic keys are intentionally string literals matching action names
package service

import (
	"context"
	"encoding/json"
	"strings"
)

func init() {
	Registry = append(Registry, opHelp)
}

type helpInput struct {
	Query string `json:"query,omitempty"`
}

var helpTopics = map[string]string{
	"query": `QUERY — find artifacts

  artifact(action=query, query="search terms")           FTS search
  artifact(action=query, ranked=true, query="...")        scored recall (kind+recency weighted)
  artifact(action=query, mode=semantic, query="...")      vector similarity (requires embeddings)
  artifact(action=query, mode=working_set, scope=myproj)  session + campaigns + ready + recent + hygiene_top
  artifact(action=query, kind=effort.task, scope=myproj)  filter by kind + scope
  artifact(action=query, status=work.active)              filter by status
  artifact(action=query, title_contains="foo")            substring match on title

See also: help(query="sort"), help(query="time"), help(query="pagination")`,

	"sort": `SORT — order query results

  artifact(action=query, sort=id)         alphabetical by ID (default)
  artifact(action=query, sort=title)      alphabetical by title
  artifact(action=query, sort=status)     group by lifecycle status
  artifact(action=query, sort=kind)       group by artifact kind
  artifact(action=query, sort=scope)      group by project scope
  artifact(action=query, sort=sprint)     group by sprint label
  artifact(action=query, sort=priority)   high → low priority
  artifact(action=query, sort=topo)       dependency order (topological)
  artifact(action=query, sort=topo, unblocked=true)  ready queue`,

	"time": `TIME FILTERS — query by timestamp

Three timestamps, all RFC3339 (e.g. 2026-07-01T00:00:00Z):

  created_at    when the artifact was first created
  updated_at    last field, section, or status change
  inserted_at   when the DB row was first written (immutable)

Examples:
  artifact(action=query, created_after="2026-07-01T00:00:00Z")
  artifact(action=query, updated_after="2026-06-30T00:00:00Z", updated_before="2026-07-01T00:00:00Z")
  artifact(action=query, kind=effort.task, updated_after="2026-06-25T00:00:00Z", sort=title)`,

	"pagination": `PAGINATION — cursor-based continuation

Results include next_cursor when more pages exist.
Pass it verbatim as cursor= to get the next page.

  # First page
  artifact(action=query, kind=effort.task, limit=10)
  # Response includes: next_cursor: "2026-06-30T12:00:00Z task-id-xyz"

  # Next page
  artifact(action=query, kind=effort.task, limit=10, cursor="2026-06-30T12:00:00Z task-id-xyz")`,

	"create": `CREATE — make a new artifact

  artifact(action=create, kind=effort.task, title="My task", scope=myproj, priority=medium,
           sections=[{name: "context", text: "Why this task exists"}])

Section keys: name/slug/title for section name, text/body for content.
Response shows "Stored sections:" (what was saved) and "Missing sections:" (what the kind expects).
Use schema(kind=X) to see required/recommended sections.`,

	"set": `SET — change a single field

  artifact(action=set, id=my-task-abc1, field=status, value=work.active)
  artifact(action=set, id=my-task-abc1, field=priority, value=high)
  artifact(action=set, id=my-task-abc1, field=title, value="New title")

Fields: status, title, goal, scope, parent, priority, sprint, kind, depends_on, labels, alias.
Use force=true to bypass lifecycle validation.
Use cascade=true to apply status to all children.
Blocked when Extra.claim is held by another agent.`,

	"export": `EXPORT — portable markdown

  artifact(action=export, id=note-abc, format=markdown)
  artifact(action=export, scope=myproj, out_dir=/tmp/export)
  artifact(action=export, scope=myproj, out_dir=/tmp/export, force=true)

Incremental: skips unchanged files; writes .conflict.md when disk is newer and differs.`,

	"claim": `CLAIM / RELEASE / HANDOFF — multi-agent leases

  artifact(action=claim, id=task-abc, agent=agent-1, ttl_seconds=3600)
  artifact(action=release, id=task-abc, agent=agent-1)
  artifact(action=handoff, artifact_id=task-abc, from_session=s1, to_session=s2, agent=agent-1, to_agent=agent-2)

Claims live in Extra.claim. Expired claims are free.`,

	"update": `UPDATE — change multiple fields and sections at once

  artifact(action=update, id=my-task-abc1, status=work.active, priority=high,
           sections=[{name: "context", text: "updated context"}])

Supports: status, title, goal, scope, parent, priority, sprint, kind, sections, sections_delete, extra.
Find-replace in sections: query="old text", text="new text".`,

	"lifecycle": `LIFECYCLE — status transitions

Use schema(kind=X) to see valid transitions for any kind.

  artifact(action=schema, kind=effort.task)

Shows default status, all valid transitions (from→to), sections, and relations.
Common effort statuses: work.draft → work.active → work.complete → retired/archived.
Use force=true on set to bypass transition validation.`,

	"relations": `RELATIONS — link artifacts

  graph(action=link, id=source-id, relation=parent_of, targets=["child-id"])
  graph(action=link, id=task-id, relation=depends_on, targets=["other-task"])
  graph(action=link, id=task-id, relation=resolves, targets=["bug-id"])

Use schema(kind=X) to see valid outbound relations and their allowed targets.
Wrong-target errors name the valid targets for that specific relation.`,

	"schema": `SCHEMA — discover kind properties or action field contracts

  artifact(action=schema, kind=effort.task)
  artifact(action=schema, name=create)   # progressive disclosure: fields for one action

Returns: lifecycle, sections, relations (incl. governed_by alias), or action-specific required/optional fields.

  artifact(action=query, kind=label_definition, scope=_schema)

Lists all registered kinds and their label definitions.`,

	"tools": `TOOLS — why the MCP schema looks huge

Three tools with action= dispatch: artifact, graph, admin.
Hosts dump every optional field on each tool — that is a flat union of kwargs, not
"fill all params". Pass only fields for the action you chose.

  artifact(action=schema, name=create)  # see only create fields
  artifact  create/get/query/set/update/... (+ export/claim/release/handoff)
  graph     link / analyze / synonym
  admin     hygiene / history / dashboard / lint / ...

Daily path: query, get, create, set, update, graph.link.
See support.doc why-scribe-mcp-looks-like-it-has-so-many-actions-6ec9.`,

	"hygiene": `HYGIENE

  admin(action=hygiene, scope=myproj)

Hygiene ranks soft graph-health findings (orphans, stale work). Status legality is enforced at write time.
Code-index kinds are excluded unless include_code=true. Intentional orphans: label hygiene:intentional_orphan
or Extra.intentional_orphan=true. Default orphan suggestion is acknowledge, not delete.`,

	"progress": `PROGRESS METRICS

content_completeness  required sections filled (docs can be 100% without delivery)
delivery_progress     lifecycle-weighted work leaves (all-draft → 0)
verified_progress     terminal leaves with evidence/verification

Stamped on get/query Extra. Dashboard shows CONT/DELV/VERF columns.`,

	"governed_by": `CANONICAL ARCHITECTURE

  graph(action=link, id=<campaign>, relation=governed_by, targets=[<decision>])

Stores as decision -justifies-> campaign. Discover via get(format=context) Canonical architecture section.
Prefer decision.accepted; label role:canonical-architecture to disambiguate.`,
}

var helpIndex string

func init() {
	var b strings.Builder
	b.WriteString("SCRIBE HELP — artifact graph commands\n\n")
	b.WriteString("Topics (use help(query=\"topic\") for details):\n\n")
	for _, topic := range []struct{ name, summary string }{
		{"query", "Find artifacts — FTS, ranked recall, filters"},
		{"sort", "Sort query results — id, title, status, kind, topo, priority"},
		{"time", "Time filters — created_after/before, updated_after/before"},
		{"pagination", "Cursor-based result continuation"},
		{"create", "Create new artifacts with sections"},
		{"set", "Change a single field (status, title, priority, ...)"},
		{"update", "Change multiple fields and sections at once"},
		{"lifecycle", "Status transitions and schema discovery"},
		{"relations", "Link artifacts with typed edges"},
		{"schema", "Discover kind properties, sections, transitions"},
		{"tools", "Why MCP lists so many fields — flat action kwargs"},
		{"hygiene", "Hygiene findings (soft graph health)"},
		{"progress", "Content vs delivery vs verified progress metrics"},
		{"governed_by", "Canonical architecture decision relation"},
	} {
		b.WriteString("  ")
		b.WriteString(topic.name)
		for i := len(topic.name); i < 14; i++ {
			b.WriteByte(' ')
		}
		b.WriteString(topic.summary)
		b.WriteByte('\n')
	}
	helpIndex = b.String()
}

var opHelp = Op{
	Name: "help",
	Run: func(_ context.Context, _ *Service, raw json.RawMessage) (string, error) {
		var in helpInput
		_ = json.Unmarshal(raw, &in)
		if in.Query == "" {
			return helpIndex, nil
		}
		topic := strings.ToLower(strings.TrimSpace(in.Query))
		if text, ok := helpTopics[topic]; ok {
			return text, nil
		}
		var matches []string
		for name, text := range helpTopics {
			if strings.Contains(text, topic) || strings.Contains(name, topic) {
				matches = append(matches, name)
			}
		}
		if len(matches) > 0 {
			return "No exact topic. Did you mean: " + strings.Join(matches, ", ") + "?\nUse help(query=\"topic\") for details.", nil
		}
		return "Unknown topic. Use help() to see available topics.", nil
	},
}
