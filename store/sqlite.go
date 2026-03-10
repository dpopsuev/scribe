package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dpopsuev/scribe/model"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS artifacts (
	id          TEXT PRIMARY KEY,
	kind        TEXT NOT NULL,
	scope       TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL,
	parent      TEXT NOT NULL DEFAULT '',
	title       TEXT NOT NULL,
	goal        TEXT NOT NULL DEFAULT '',
	depends_on  TEXT NOT NULL DEFAULT '[]',
	labels      TEXT NOT NULL DEFAULT '[]',
	priority    TEXT NOT NULL DEFAULT '',
	sprint      TEXT NOT NULL DEFAULT '',
	sections    TEXT NOT NULL DEFAULT '[]',
	features    TEXT NOT NULL DEFAULT '[]',
	criteria    TEXT NOT NULL DEFAULT '[]',
	links       TEXT NOT NULL DEFAULT '{}',
	extra       TEXT NOT NULL DEFAULT '{}',
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL,
	inserted_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_art_kind   ON artifacts(kind);
CREATE INDEX IF NOT EXISTS idx_art_scope  ON artifacts(scope);
CREATE INDEX IF NOT EXISTS idx_art_status ON artifacts(status);
CREATE INDEX IF NOT EXISTS idx_art_parent ON artifacts(parent);
CREATE INDEX IF NOT EXISTS idx_art_sprint ON artifacts(sprint);

CREATE TABLE IF NOT EXISTS edges (
	from_id  TEXT NOT NULL,
	relation TEXT NOT NULL,
	to_id    TEXT NOT NULL,
	PRIMARY KEY (from_id, relation, to_id)
);
CREATE INDEX IF NOT EXISTS idx_edges_rev ON edges(to_id, relation, from_id);

CREATE TABLE IF NOT EXISTS sequences (
	prefix   TEXT PRIMARY KEY,
	next_val INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS scope_keys (
	scope   TEXT PRIMARY KEY,
	key     TEXT UNIQUE NOT NULL,
	auto    INTEGER NOT NULL DEFAULT 1,
	created TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS scoped_sequences (
	scope_key TEXT NOT NULL,
	kind_code TEXT NOT NULL,
	next_val  INTEGER NOT NULL DEFAULT 1,
	PRIMARY KEY (scope_key, kind_code)
);
`

// DefaultSQLitePath returns the default database path.
// Resolution: $SCRIBE_ROOT/scribe.sqlite > ~/.scribe/scribe.sqlite.
func DefaultSQLitePath() string {
	if root := os.Getenv("SCRIBE_ROOT"); root != "" {
		return filepath.Join(root, "scribe.sqlite")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".scribe", "scribe.sqlite")
}

// SQLiteStore implements Store on top of SQLite with WAL mode.
type SQLiteStore struct {
	writer *sql.DB
	reader *sql.DB
}

// OpenSQLite creates or opens a SQLite database at path with WAL mode.
func OpenSQLite(path string) (*SQLiteStore, error) {
	log := slog.With("component", "store", "path", path)

	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return nil, fmt.Errorf("db path %s is a directory, not a file", path)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	dsn := path + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"

	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	writer.SetMaxOpenConns(1)

	if _, err := writer.Exec(schema); err != nil {
		writer.Close()
		log.Error("schema creation failed", "error", err)
		return nil, fmt.Errorf("create schema: %w", err)
	}

	writer.ExecContext(context.Background(),
		"ALTER TABLE artifacts ADD COLUMN inserted_at TEXT NOT NULL DEFAULT ''")
	writer.ExecContext(context.Background(),
		"UPDATE artifacts SET inserted_at = created_at WHERE inserted_at = ''")

	reader, err := sql.Open("sqlite", dsn+"&_pragma=query_only(on)")
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	reader.SetMaxOpenConns(4)

	log.Info("database opened")
	return &SQLiteStore{writer: writer, reader: reader}, nil
}

func (s *SQLiteStore) Close() error {
	s.reader.Close()
	return s.writer.Close()
}

// DBSizeBytes returns the approximate database file size using PRAGMA page_count/page_size.
func (s *SQLiteStore) DBSizeBytes(ctx context.Context) (int64, error) {
	var pageCount, pageSize int64
	if err := s.reader.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return 0, err
	}
	if err := s.reader.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, err
	}
	return pageCount * pageSize, nil
}

func (s *SQLiteStore) Put(ctx context.Context, art *model.Artifact) error {
	if art.ID == "" {
		return fmt.Errorf("artifact ID is required")
	}
	now := time.Now().UTC()
	if art.CreatedAt.IsZero() {
		art.CreatedAt = now
	}
	art.UpdatedAt = now
	if art.InsertedAt.IsZero() {
		art.InsertedAt = now
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var old *model.Artifact
	old, _ = scanArtifact(tx.QueryRowContext(ctx, "SELECT * FROM artifacts WHERE id = ?", art.ID))

	dependsOn, _ := json.Marshal(art.DependsOn)
	labels, _ := json.Marshal(art.Labels)
	sections, _ := json.Marshal(art.Sections)
	features, _ := json.Marshal(art.Features)
	criteria, _ := json.Marshal(art.Criteria)
	links, _ := json.Marshal(art.Links)
	extra, _ := json.Marshal(art.Extra)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO artifacts (id, kind, scope, status, parent, title, goal, depends_on, labels, priority, sprint, sections, features, criteria, links, extra, created_at, updated_at, inserted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			kind=excluded.kind, scope=excluded.scope, status=excluded.status,
			parent=excluded.parent, title=excluded.title, goal=excluded.goal,
			depends_on=excluded.depends_on, labels=excluded.labels,
			priority=excluded.priority, sprint=excluded.sprint,
			sections=excluded.sections, features=excluded.features,
			criteria=excluded.criteria, links=excluded.links,
			extra=excluded.extra, updated_at=excluded.updated_at`,
		art.ID, art.Kind, art.Scope, art.Status, art.Parent, art.Title, art.Goal,
		string(dependsOn), string(labels), art.Priority, art.Sprint,
		string(sections), string(features), string(criteria), string(links), string(extra),
		art.CreatedAt.Format(time.RFC3339Nano), art.UpdatedAt.Format(time.RFC3339Nano),
		art.InsertedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert %s: %w", art.ID, err)
	}

	if err := reconcileEdgesSQL(ctx, tx, old, art); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*model.Artifact, error) {
	row := s.reader.QueryRowContext(ctx, "SELECT * FROM artifacts WHERE id = ?", id)
	art, err := scanArtifact(row)
	if err != nil {
		return nil, fmt.Errorf("artifact %s not found", id)
	}
	return art, nil
}

func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, "DELETE FROM artifacts WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("artifact %s not found", id)
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM edges WHERE from_id = ? OR to_id = ?", id, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) List(ctx context.Context, f model.Filter) ([]*model.Artifact, error) {
	var clauses []string
	var args []any
	if f.IDPrefix != "" {
		clauses = append(clauses, "id LIKE ?")
		args = append(args, f.IDPrefix+"%")
	}
	if f.Kind != "" {
		clauses = append(clauses, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.ExcludeKind != "" {
		clauses = append(clauses, "kind != ?")
		args = append(args, f.ExcludeKind)
	}
	if f.ExcludeStatus != "" {
		clauses = append(clauses, "status != ?")
		args = append(args, f.ExcludeStatus)
	}
	if len(f.ExcludeKinds) > 0 {
		ph := make([]string, len(f.ExcludeKinds))
		for i, ek := range f.ExcludeKinds {
			ph[i] = "?"
			args = append(args, ek)
		}
		clauses = append(clauses, "kind NOT IN ("+strings.Join(ph, ",")+")")
	}
	if len(f.Scopes) > 0 {
		placeholders := make([]string, len(f.Scopes))
		for i, sc := range f.Scopes {
			placeholders[i] = "?"
			args = append(args, sc)
		}
		clauses = append(clauses, "scope IN ("+strings.Join(placeholders, ",")+")")
	} else if f.Scope != "" {
		clauses = append(clauses, "scope = ?")
		args = append(args, f.Scope)
	}
	if f.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, f.Status)
	}
	if f.Parent != "" {
		clauses = append(clauses, "parent = ?")
		args = append(args, f.Parent)
	}
	if f.Sprint != "" {
		clauses = append(clauses, "sprint = ?")
		args = append(args, f.Sprint)
	}
	if f.CreatedAfter != "" {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, f.CreatedAfter)
	}
	if f.CreatedBefore != "" {
		clauses = append(clauses, "created_at < ?")
		args = append(args, f.CreatedBefore)
	}
	if f.UpdatedAfter != "" {
		clauses = append(clauses, "updated_at >= ?")
		args = append(args, f.UpdatedAfter)
	}
	if f.UpdatedBefore != "" {
		clauses = append(clauses, "updated_at < ?")
		args = append(args, f.UpdatedBefore)
	}
	if f.InsertedAfter != "" {
		clauses = append(clauses, "inserted_at >= ?")
		args = append(args, f.InsertedAfter)
	}
	if f.InsertedBefore != "" {
		clauses = append(clauses, "inserted_at < ?")
		args = append(args, f.InsertedBefore)
	}

	q := "SELECT * FROM artifacts"
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY id"

	rows, err := s.reader.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*model.Artifact
	for rows.Next() {
		art, err := scanArtifactRows(rows)
		if err != nil {
			continue
		}
		if len(f.Labels) > 0 {
			have := make(map[string]bool, len(art.Labels))
			for _, l := range art.Labels {
				have[l] = true
			}
			ok := true
			for _, want := range f.Labels {
				if !have[want] {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		results = append(results, art)
	}
	return results, rows.Err()
}

func (s *SQLiteStore) AddEdge(ctx context.Context, e model.Edge) error {
	_, err := s.writer.ExecContext(ctx,
		"INSERT OR IGNORE INTO edges (from_id, relation, to_id) VALUES (?, ?, ?)",
		e.From, e.Relation, e.To)
	return err
}

func (s *SQLiteStore) RemoveEdge(ctx context.Context, e model.Edge) error {
	_, err := s.writer.ExecContext(ctx,
		"DELETE FROM edges WHERE from_id = ? AND relation = ? AND to_id = ?",
		e.From, e.Relation, e.To)
	return err
}

func (s *SQLiteStore) Neighbors(ctx context.Context, id string, rel string, dir Direction) ([]model.Edge, error) {
	var edges []model.Edge

	if dir == Outgoing || dir == Both {
		q := "SELECT from_id, relation, to_id FROM edges WHERE from_id = ?"
		args := []any{id}
		if rel != "" {
			q += " AND relation = ?"
			args = append(args, rel)
		}
		rows, err := s.reader.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e model.Edge
			if err := rows.Scan(&e.From, &e.Relation, &e.To); err == nil {
				edges = append(edges, e)
			}
		}
		rows.Close()
	}

	if dir == Incoming || dir == Both {
		q := "SELECT from_id, relation, to_id FROM edges WHERE to_id = ?"
		args := []any{id}
		if rel != "" {
			q += " AND relation = ?"
			args = append(args, rel)
		}
		rows, err := s.reader.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var e model.Edge
			if err := rows.Scan(&e.From, &e.Relation, &e.To); err == nil {
				edges = append(edges, e)
			}
		}
		rows.Close()
	}

	return edges, nil
}

func (s *SQLiteStore) Walk(ctx context.Context, root string, rel string, dir Direction, maxDepth int, fn WalkFn) error {
	visited := make(map[string]bool)
	return s.walkRecurse(ctx, root, rel, dir, maxDepth, 0, visited, fn)
}

func (s *SQLiteStore) walkRecurse(ctx context.Context, id string, rel string, dir Direction, maxDepth, depth int, visited map[string]bool, fn WalkFn) error {
	if maxDepth > 0 && depth >= maxDepth {
		return nil
	}
	if visited[id] {
		return nil
	}
	visited[id] = true

	neighbors, err := s.Neighbors(ctx, id, rel, dir)
	if err != nil {
		return err
	}
	for _, e := range neighbors {
		if !fn(depth+1, e) {
			return nil
		}
		next := e.To
		if dir == Incoming {
			next = e.From
		}
		if err := s.walkRecurse(ctx, next, rel, dir, maxDepth, depth+1, visited, fn); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Children(ctx context.Context, parentID string) ([]*model.Artifact, error) {
	edges, err := s.Neighbors(ctx, parentID, model.RelParentOf, Outgoing)
	if err != nil {
		return nil, err
	}
	var children []*model.Artifact
	for _, e := range edges {
		if child, err := s.Get(ctx, e.To); err == nil {
			children = append(children, child)
		}
	}
	return children, nil
}

func (s *SQLiteStore) NextID(ctx context.Context, prefix string) (string, error) {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var seq int64
	err = tx.QueryRowContext(ctx, "SELECT next_val FROM sequences WHERE prefix = ?", prefix).Scan(&seq)
	if err == sql.ErrNoRows {
		seq = 1
	} else if err != nil {
		return "", err
	}

	id := model.FormatID(prefix, int(seq))

	_, err = tx.ExecContext(ctx,
		"INSERT INTO sequences (prefix, next_val) VALUES (?, ?) ON CONFLICT(prefix) DO UPDATE SET next_val = ?",
		prefix, seq+1, seq+1)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *SQLiteStore) SeedSequence(ctx context.Context, prefix string, val uint64, force bool) error {
	if force {
		_, err := s.writer.ExecContext(ctx,
			"INSERT INTO sequences (prefix, next_val) VALUES (?, ?) ON CONFLICT(prefix) DO UPDATE SET next_val = ?",
			prefix, val, val)
		return err
	}
	_, err := s.writer.ExecContext(ctx,
		`INSERT INTO sequences (prefix, next_val) VALUES (?, ?)
		 ON CONFLICT(prefix) DO UPDATE SET next_val = MAX(sequences.next_val, excluded.next_val)`,
		prefix, val)
	return err
}

func (s *SQLiteStore) NextScopedID(ctx context.Context, scopeKey, kindCode string) (string, error) {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var seq int64
	err = tx.QueryRowContext(ctx,
		"SELECT next_val FROM scoped_sequences WHERE scope_key = ? AND kind_code = ?",
		scopeKey, kindCode).Scan(&seq)
	if err == sql.ErrNoRows {
		seq = 1
	} else if err != nil {
		return "", err
	}

	id := model.FormatScopedID(scopeKey, kindCode, int(seq))

	_, err = tx.ExecContext(ctx,
		`INSERT INTO scoped_sequences (scope_key, kind_code, next_val) VALUES (?, ?, ?)
		 ON CONFLICT(scope_key, kind_code) DO UPDATE SET next_val = ?`,
		scopeKey, kindCode, seq+1, seq+1)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *SQLiteStore) GetScopeKey(ctx context.Context, scope string) (string, bool, error) {
	var key string
	var auto int
	err := s.reader.QueryRowContext(ctx,
		"SELECT key, auto FROM scope_keys WHERE scope = ?", scope).Scan(&key, &auto)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return key, auto == 1, nil
}

func (s *SQLiteStore) SetScopeKey(ctx context.Context, scope, key string, auto bool) error {
	autoInt := 0
	if auto {
		autoInt = 1
	}
	_, err := s.writer.ExecContext(ctx,
		`INSERT INTO scope_keys (scope, key, auto, created) VALUES (?, ?, ?, ?)
		 ON CONFLICT(scope) DO UPDATE SET key = excluded.key, auto = excluded.auto`,
		scope, key, autoInt, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *SQLiteStore) ListScopeKeys(ctx context.Context) (map[string]string, error) {
	rows, err := s.reader.QueryContext(ctx, "SELECT scope, key FROM scope_keys")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var scope, key string
		if err := rows.Scan(&scope, &key); err == nil {
			result[scope] = key
		}
	}
	return result, rows.Err()
}

// --- scan helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanArtifact(row *sql.Row) (*model.Artifact, error) {
	return scanRow(row)
}

func scanArtifactRows(rows *sql.Rows) (*model.Artifact, error) {
	return scanRow(rows)
}

func scanRow(s rowScanner) (*model.Artifact, error) {
	var art model.Artifact
	var dependsOn, labels, sections, features, criteria, links, extra string
	var createdAt, updatedAt, insertedAt string

	err := s.Scan(
		&art.ID, &art.Kind, &art.Scope, &art.Status, &art.Parent, &art.Title, &art.Goal,
		&dependsOn, &labels, &art.Priority, &art.Sprint,
		&sections, &features, &criteria, &links, &extra,
		&createdAt, &updatedAt, &insertedAt,
	)
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(dependsOn), &art.DependsOn)
	json.Unmarshal([]byte(labels), &art.Labels)
	json.Unmarshal([]byte(sections), &art.Sections)
	json.Unmarshal([]byte(features), &art.Features)
	json.Unmarshal([]byte(criteria), &art.Criteria)
	json.Unmarshal([]byte(links), &art.Links)
	json.Unmarshal([]byte(extra), &art.Extra)
	art.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	art.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	art.InsertedAt, _ = time.Parse(time.RFC3339Nano, insertedAt)

	if art.DependsOn == nil {
		art.DependsOn = nil
	}
	if art.Labels == nil {
		art.Labels = nil
	}

	return &art, nil
}

// reconcileEdgesSQL mirrors the bbolt reconcileEdges logic using SQL.
func reconcileEdgesSQL(ctx context.Context, tx *sql.Tx, old, cur *model.Artifact) error {
	oldParent := ""
	if old != nil {
		oldParent = old.Parent
	}
	if cur.Parent != oldParent {
		if oldParent != "" {
			tx.ExecContext(ctx, "DELETE FROM edges WHERE from_id = ? AND relation = ? AND to_id = ?",
				oldParent, model.RelParentOf, cur.ID)
		}
		if cur.Parent != "" {
			tx.ExecContext(ctx, "INSERT OR IGNORE INTO edges (from_id, relation, to_id) VALUES (?, ?, ?)",
				cur.Parent, model.RelParentOf, cur.ID)
		}
	}

	oldDeps := make(map[string]bool)
	if old != nil {
		for _, d := range old.DependsOn {
			oldDeps[d] = true
		}
	}
	newDeps := make(map[string]bool)
	for _, d := range cur.DependsOn {
		newDeps[d] = true
	}
	for d := range oldDeps {
		if !newDeps[d] {
			tx.ExecContext(ctx, "DELETE FROM edges WHERE from_id = ? AND relation = ? AND to_id = ?",
				cur.ID, model.RelDependsOn, d)
		}
	}
	for d := range newDeps {
		if !oldDeps[d] {
			tx.ExecContext(ctx, "INSERT OR IGNORE INTO edges (from_id, relation, to_id) VALUES (?, ?, ?)",
				cur.ID, model.RelDependsOn, d)
		}
	}

	oldLinks := make(map[string]map[string]bool)
	if old != nil {
		for rel, ids := range old.Links {
			s := make(map[string]bool)
			for _, id := range ids {
				s[id] = true
			}
			oldLinks[rel] = s
		}
	}
	newLinks := make(map[string]map[string]bool)
	for rel, ids := range cur.Links {
		s := make(map[string]bool)
		for _, id := range ids {
			s[id] = true
		}
		newLinks[rel] = s
	}
	for rel, oldSet := range oldLinks {
		newSet := newLinks[rel]
		for id := range oldSet {
			if newSet == nil || !newSet[id] {
				tx.ExecContext(ctx, "DELETE FROM edges WHERE from_id = ? AND relation = ? AND to_id = ?",
					cur.ID, rel, id)
			}
		}
	}
	for rel, newSet := range newLinks {
		oldSet := oldLinks[rel]
		for id := range newSet {
			if oldSet == nil || !oldSet[id] {
				tx.ExecContext(ctx, "INSERT OR IGNORE INTO edges (from_id, relation, to_id) VALUES (?, ?, ?)",
					cur.ID, rel, id)
			}
		}
	}
	return nil
}
