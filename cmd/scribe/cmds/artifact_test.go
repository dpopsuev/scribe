package cmds_test

import (
	"strings"
	"testing"
)

func TestCreate_PrintsID(t *testing.T) {
	db := newDB(t)
	out := run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "write tests")
	if !strings.Contains(out, "TSK") {
		t.Errorf("expected task ID in output, got: %q", out)
	}
}

func TestShow_ReturnsArtifact(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "show me"))
	out := run(t, db, "show", id)
	mustContain(t, out, "show me")
}

func TestList_FiltersByKind(t *testing.T) {
	db := newDB(t)
	run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "a task")
	run(t, db, "create", "--kind", "note", "--scope", "test", "--title", "a note")

	out := run(t, db, "list", "--kind", "task")
	mustContain(t, out, "a task")
	mustNotContain(t, out, "a note")
}

func TestList_FiltersByScope(t *testing.T) {
	db := newDB(t)
	run(t, db, "create", "--kind", "task", "--scope", "alpha", "--title", "alpha task")
	run(t, db, "create", "--kind", "task", "--scope", "beta", "--title", "beta task")

	out := run(t, db, "list", "--scope", "alpha")
	mustContain(t, out, "alpha task")
	mustNotContain(t, out, "beta task")
}

func TestSet_ChangesField(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "before"))
	run(t, db, "set", id, "title", "after")
	out := run(t, db, "show", id)
	mustContain(t, out, "after")
	mustNotContain(t, out, "before")
}

func TestArchive_ChangesStatus(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "to archive"))
	run(t, db, "archive", id)
	out := run(t, db, "show", id)
	mustContain(t, out, "archived")
}

func TestDelete_RemovesArtifact(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "task", "--scope", "test", "--title", "delete me"))
	run(t, db, "archive", id)
	run(t, db, "delete", id)
	out := run(t, db, "show", id)
	mustNotContain(t, out, "delete me")
}

func TestSection_AddAndShow(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "note", "--scope", "test", "--title", "noted"))
	run(t, db, "section", "add", id, "summary", "the summary text")
	out := run(t, db, "section", "show", id, "summary")
	mustContain(t, out, "the summary text")
}
