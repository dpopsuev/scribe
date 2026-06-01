package cmds_test

import (
	"strings"
	"testing"
)

func TestLink_CreatesEdge(t *testing.T) {
	db := newDB(t)
	parent := strings.TrimSpace(run(t, db, "create", "--kind", "spec", "--scope", "test", "--title", "the spec"))
	child := strings.TrimSpace(run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "the task"))

	run(t, db, "link", child, "implements", parent)
	out := run(t, db, "briefing", child)
	mustContain(t, out, parent)
}

func TestUnlink_RemovesEdge(t *testing.T) {
	db := newDB(t)
	parent := strings.TrimSpace(run(t, db, "create", "--kind", "spec", "--scope", "test", "--title", "the spec"))
	child := strings.TrimSpace(run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "the task"))

	run(t, db, "link", child, "implements", parent)
	run(t, db, "unlink", child, "implements", parent)
	out := run(t, db, "briefing", child)
	mustNotContain(t, out, parent)
}

func TestTree_ShowsChildren(t *testing.T) {
	db := newDB(t)
	parent := strings.TrimSpace(run(t, db, "create", "--kind", "goal", "--scope", "test", "--title", "the goal"))
	child := strings.TrimSpace(run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "child task", "--parent", parent))

	out := run(t, db, "tree", parent)
	mustContain(t, out, child)
	mustContain(t, out, "child task")
}

func TestBriefing_ShowsContext(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "briefed task"))
	out := run(t, db, "briefing", id)
	mustContain(t, out, "briefed task")
}

func TestSearch_FindsByQuery(t *testing.T) {
	db := newDB(t)
	run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "auth implementation")
	run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "database migration")

	out := run(t, db, "search", "auth")
	mustContain(t, out, "auth implementation")
	mustNotContain(t, out, "database migration")
}
