package cmds_test

import (
	"strings"
	"testing"
)

func TestCreate_PrintsID(t *testing.T) {
	db := newDB(t)
	out := run(t, db, "create", "--kind", "effort.task", "--scope", "test", "--title", "write tests")
	id := strings.TrimSpace(out)
	if id == "" {
		t.Errorf("expected artifact ID in output, got empty")
	}
	if !strings.HasPrefix(id, "write-tests-") {
		t.Errorf("expected slug starting with write-tests-, got: %q", id)
	}
}

func TestShow_ReturnsArtifact(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "effort.task", "--scope", "test", "--title", "show me"))
	out := run(t, db, "show", id)
	mustContain(t, out, "show me")
}

func TestList_FiltersByKind(t *testing.T) {
	db := newDB(t)
	run(t, db, "create", "--kind", "effort.task", "--scope", "test", "--title", "a task")
	run(t, db, "create", "--kind", "knowledge.note", "--scope", "test", "--title", "a note")

	out := run(t, db, "list", "--kind", "effort.task")
	mustContain(t, out, "a task")
	mustNotContain(t, out, "a note")
}

func TestList_FiltersByScope(t *testing.T) {
	db := newDB(t)
	run(t, db, "create", "--kind", "effort.task", "--scope", "alpha", "--title", "alpha task")
	run(t, db, "create", "--kind", "effort.task", "--scope", "beta", "--title", "beta task")

	out := run(t, db, "list", "--scope", "alpha")
	mustContain(t, out, "alpha task")
	mustNotContain(t, out, "beta task")
}

func TestSet_ChangesField(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "effort.task", "--scope", "test", "--title", "original title"))
	run(t, db, "set", id, "title", "renamed title")
	out := run(t, db, "show", id)
	mustContain(t, out, "renamed title")
}

func TestDelete_RemovesArtifact(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "effort.task", "--scope", "test", "--title", "delete me"))
	run(t, db, "delete", id)
	out := run(t, db, "show", id)
	mustNotContain(t, out, "delete me")
}

func TestSection_AddAndShow(t *testing.T) {
	db := newDB(t)
	id := strings.TrimSpace(run(t, db, "create", "--kind", "knowledge.note", "--scope", "test", "--title", "noted"))
	run(t, db, "section", "add", id, "summary", "the summary text")
	out := run(t, db, "section", "show", id, "summary")
	mustContain(t, out, "the summary text")
}
