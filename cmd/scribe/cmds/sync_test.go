package cmds_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSync_ImportsMarkdownFiles(t *testing.T) {
	db := newDB(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "rule.md"), []byte(`---
id: RULE-CLI-001
kind: note
title: Keep it simple
scope: lexicon
labels: [rule]
---

## content

Prefer simple solutions over clever ones.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := run(t, db, "sync", dir)
	mustContain(t, out, "1")

	// Verify artifact is retrievable via show
	showOut := run(t, db, "show", "RULE-CLI-001")
	mustContain(t, showOut, "Keep it simple")
}

func TestSync_IdempotentOnRerun(t *testing.T) {
	db := newDB(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte(`---
id: SYNC-IDEM-001
kind: note
title: Stable note
scope: test
---
`), 0o644); err != nil {
		t.Fatal(err)
	}

	run(t, db, "sync", dir)
	out := run(t, db, "sync", dir)
	// Second sync should report 1 (upsert, not error)
	mustContain(t, out, "1")
}
