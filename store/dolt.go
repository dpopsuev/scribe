package store

// dolt.go — DoltStore: parchment.Store backed by embedded Dolt.
//
// Dolt is a MySQL-compatible version-controlled database. It provides:
//   - DOLT_BRANCH per agent session → isolated knowledge deposits
//   - DOLT_MERGE to promote reviewed sessions to main
//   - DOLT_DIFF to audit what an agent deposited
//   - VECTOR INDEX for semantic search at O(log n) (future)
//
// Connection: embedded in-process via dolthub/driver (no daemon needed).
// DSN: file://<dir>?commitname=<name>&commitemail=<email>
//
// SQL compatibility: Dolt is MySQL-compatible. Key differences from SQLite:
//   INSERT OR REPLACE   → INSERT ... ON DUPLICATE KEY UPDATE
//   INSERT OR IGNORE    → INSERT IGNORE
//   AUTOINCREMENT       → AUTO_INCREMENT
//   TEXT                → LONGTEXT (MySQL)
//   json_group_array()  → JSON_ARRAYAGG()
//   FTS5 MATCH          → FULLTEXT MATCH IN BOOLEAN MODE

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	_ "github.com/dolthub/driver" // registers "dolt" SQL driver
	parchment "github.com/dpopsuev/parchment"
)

const (
	doltCommitName  = "scribe-agent"
	doltCommitEmail = "scribe@agent.local"
	doltDBName      = "scribe"
)

// DoltStore implements parchment.Store backed by embedded Dolt.
type DoltStore struct {
	db     *sql.DB
	branch string // current branch ("main" for primary, session name for agents)
	dir    string
}

// OpenDolt opens (or creates) a Dolt store at dir on the main branch.
func OpenDolt(dir string) (*DoltStore, error) {
	return openDolt(dir, "main")
}

// OpenDoltSession opens a Dolt store at dir on a session-specific branch.
// The branch is created if it doesn't exist. All writes go to this branch.
func OpenDoltSession(dir, sessionName string) (*DoltStore, error) {
	s, err := openDolt(dir, sessionName)
	if err != nil {
		return nil, err
	}
	// Create branch if needed and checkout.
	if sessionName != "main" {
		if err := s.ensureBranch(sessionName); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("ensure branch %q: %w", sessionName, err)
		}
	}
	return s, nil
}

func openDolt(dir, branch string) (*DoltStore, error) {
	dsn := fmt.Sprintf("file://%s?commitname=%s&commitemail=%s",
		dir, doltCommitName, doltCommitEmail)
	db, err := sql.Open("dolt", dsn)
	if err != nil {
		return nil, fmt.Errorf("open dolt: %w", err)
	}
	// Single connection: Dolt branch state (DOLT_CHECKOUT) is per-connection.
	// All operations must share the same connection to see the same branch.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &DoltStore{db: db, branch: branch, dir: dir}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *DoltStore) init() error {
	ctx := context.Background()

	// Create database if needed.
	if _, err := s.db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+doltDBName); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "USE "+doltDBName); err != nil {
		return fmt.Errorf("use database: %w", err)
	}

	// Create schema.
	for _, ddl := range doltSchema {
		if _, err := s.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("create schema (%s...): %w", ddl[:func() int {
				if 40 < len(ddl) {
					return 40
				}
				return len(ddl)
			}()], err)
		}
	}

	// Always ensure an initial commit exists on main so branches can be created.
	if err := s.initialCommit(ctx); err != nil {
		return err
	}
	return nil
}

func (s *DoltStore) initialCommit(ctx context.Context) error {
	// Always stage and commit — DOLT_COMMIT --allow-empty is idempotent.
	// We cannot rely on dolt_log existing on a brand-new database.
	if _, err := s.db.ExecContext(ctx, "CALL DOLT_ADD('-A')"); err != nil {
		// Ignore: nothing staged is fine on a fresh empty database.
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "CALL DOLT_COMMIT('--allow-empty', '-m', 'init: schema')"); err != nil {
		// Ignore if already committed or nothing to commit.
		return nil //nolint:nilerr // commit errors are non-fatal on init
	}
	return nil
}

func (s *DoltStore) ensureBranch(name string) error {
	ctx := context.Background()
	// Try to create; ignore error if already exists.
	_, _ = s.db.ExecContext(ctx, "CALL DOLT_BRANCH(?)", name)
	if _, err := s.db.ExecContext(ctx, "CALL DOLT_CHECKOUT(?)", name); err != nil {
		return fmt.Errorf("checkout %q: %w", name, err)
	}
	return nil
}

// --- Session operations (Dolt-specific) ---

// Commit stages all changes and creates a Dolt commit on the current branch.
func (s *DoltStore) Commit(ctx context.Context, message string) error {
	if _, err := s.db.ExecContext(ctx, "CALL DOLT_ADD('-A')"); err != nil {
		return fmt.Errorf("dolt add: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "CALL DOLT_COMMIT('--allow-empty', '-m', ?)", message); err != nil {
		return fmt.Errorf("dolt commit: %w", err)
	}
	return nil
}

// DiffFromMain returns a human-readable summary of changes vs main.
// Uses the dolt_diff() table function: dolt_diff('main', branch, table).
func (s *DoltStore) DiffFromMain(ctx context.Context) ([]string, error) {
	// Dolt table functions don't support ? placeholders — use safe string interpolation.
	// Branch name is controlled by our own code, not user input.
	q := fmt.Sprintf( //nolint:gosec // branch name is operator-controlled, not user input
		"SELECT diff_type, to_id, to_title FROM dolt_diff('main', '%s', 'artifacts')", s.branch)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, nil //nolint:nilerr // graceful degradation — diff not critical
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query

	var lines []string
	for rows.Next() {
		var diffType, id, title string
		if err := rows.Scan(&diffType, &id, &title); err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s %q", diffType, id, title))
	}
	return lines, nil
}

// MergeToMain merges the current session branch into main.
func (s *DoltStore) MergeToMain(ctx context.Context) error {
	branch := s.branch
	// Switch to main.
	if _, err := s.db.ExecContext(ctx, "CALL DOLT_CHECKOUT('main')"); err != nil {
		return fmt.Errorf("checkout main: %w", err)
	}
	// Merge session branch.
	if _, err := s.db.ExecContext(ctx, "CALL DOLT_MERGE(?)", branch); err != nil {
		return fmt.Errorf("merge %q: %w", branch, err)
	}
	s.branch = "main"
	return nil
}

// Branch returns the current branch name.
func (s *DoltStore) Branch() string { return s.branch }

// Close closes the database connection.
func (s *DoltStore) Close() error {
	return s.db.Close()
}

// --- ArtifactStore ---

func (s *DoltStore) Put(ctx context.Context, art *parchment.Artifact) error {
	sections, _ := json.Marshal(art.Sections)
	links, _ := json.Marshal(art.Links)
	extra, _ := json.Marshal(art.Extra)

	now := time.Now().UTC()
	if art.CreatedAt.IsZero() {
		art.CreatedAt = now
	}
	art.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO artifacts
			(uid, id, alias, kind, scope, status, parent, title, goal,
			 depends_on, labels, priority, sprint, sections, links, extra,
			 created_at, updated_at, inserted_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
			uid=VALUES(uid), alias=VALUES(alias), kind=VALUES(kind),
			scope=VALUES(scope), status=VALUES(status), parent=VALUES(parent),
			title=VALUES(title), goal=VALUES(goal), depends_on=VALUES(depends_on),
			labels=VALUES(labels), priority=VALUES(priority), sprint=VALUES(sprint),
			sections=VALUES(sections), links=VALUES(links), extra=VALUES(extra),
			updated_at=VALUES(updated_at)`,
		art.UID, art.ID, art.Alias, art.Kind, art.Scope, art.Status,
		art.Parent, art.Title, art.Goal,
		joinStr(art.DependsOn), joinStr(art.Labels),
		art.Priority, art.Sprint,
		string(sections), string(links), string(extra),
		art.CreatedAt.Format(time.RFC3339),
		art.UpdatedAt.Format(time.RFC3339),
		now.Format(time.RFC3339),
	)
	return err
}

func (s *DoltStore) Get(ctx context.Context, id string) (*parchment.Artifact, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT uid,id,alias,kind,scope,status,parent,title,goal,
		        depends_on,labels,priority,sprint,sections,links,extra,
		        created_at,updated_at,inserted_at
		 FROM artifacts WHERE id=?`, id)
	art, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", parchment.ErrArtifactNotFound, id)
	}
	return art, err
}

func (s *DoltStore) GetByAlias(ctx context.Context, alias string) (*parchment.Artifact, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT uid,id,alias,kind,scope,status,parent,title,goal,
		        depends_on,labels,priority,sprint,sections,links,extra,
		        created_at,updated_at,inserted_at
		 FROM artifacts WHERE alias=?`, alias)
	art, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: alias=%s", parchment.ErrArtifactNotFound, alias)
	}
	return art, err
}

func (s *DoltStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM artifacts WHERE id=?", id)
	return err
}

func (s *DoltStore) List(ctx context.Context, f parchment.Filter) ([]*parchment.Artifact, error) { //nolint:gocritic // hugeParam: Filter value semantics match parchment.SQLiteStore pattern
	q := `SELECT uid,id,alias,kind,scope,status,parent,title,goal,
	             depends_on,labels,priority,sprint,sections,links,extra,
	             created_at,updated_at,inserted_at
	      FROM artifacts WHERE 1=1`
	var args []any
	if f.Kind != "" {
		q += " AND kind=?"
		args = append(args, f.Kind)
	}
	if f.Scope != "" {
		q += " AND scope=?"
		args = append(args, f.Scope)
	}
	if f.Status != "" {
		q += " AND status=?"
		args = append(args, f.Status)
	}
	if f.Parent != "" {
		q += " AND parent=?"
		args = append(args, f.Parent)
	}
	if len(f.Scopes) > 0 {
		placeholders := strings.Repeat("?,", len(f.Scopes))
		q += " AND scope IN (" + placeholders[:len(placeholders)-1] + ")" //nolint:gosec // placeholders are "?,?,?" — no user data
		for _, sc := range f.Scopes {
			args = append(args, sc)
		}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query
	return scanArtifacts(rows)
}

func (s *DoltStore) Children(ctx context.Context, parentID string) ([]*parchment.Artifact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT uid,id,alias,kind,scope,status,parent,title,goal,
		        depends_on,labels,priority,sprint,sections,links,extra,
		        created_at,updated_at,inserted_at
		 FROM artifacts WHERE parent=?`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query
	return scanArtifacts(rows)
}

func (s *DoltStore) Search(ctx context.Context, query string) ([]string, error) {
	// Dolt FULLTEXT support varies — use LIKE as reliable fallback.
	q := "%" + query + "%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM artifacts WHERE title LIKE ? OR goal LIKE ? OR sections LIKE ? ORDER BY updated_at DESC LIMIT 50`,
		q, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// --- GraphStore ---

func (s *DoltStore) AddEdge(ctx context.Context, e parchment.Edge) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT IGNORE INTO edges (from_id, relation, to_id) VALUES (?,?,?)`,
		e.From, e.Relation, e.To)
	return err
}

func (s *DoltStore) RemoveEdge(ctx context.Context, e parchment.Edge) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM edges WHERE from_id=? AND relation=? AND to_id=?`,
		e.From, e.Relation, e.To)
	return err
}

func (s *DoltStore) Neighbors(ctx context.Context, id, rel string, dir parchment.Direction) ([]parchment.Edge, error) {
	var rows *sql.Rows
	var err error
	switch dir {
	case parchment.Outgoing:
		rows, err = s.db.QueryContext(ctx,
			`SELECT from_id, relation, to_id FROM edges WHERE from_id=? AND (relation=? OR ?='')`,
			id, rel, rel)
	case parchment.Incoming:
		rows, err = s.db.QueryContext(ctx,
			`SELECT from_id, relation, to_id FROM edges WHERE to_id=? AND (relation=? OR ?='')`,
			id, rel, rel)
	default:
		rows, err = s.db.QueryContext(ctx,
			`SELECT from_id, relation, to_id FROM edges WHERE (from_id=? OR to_id=?) AND (relation=? OR ?='')`,
			id, id, rel, rel)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query
	var edges []parchment.Edge
	for rows.Next() {
		var e parchment.Edge
		if err := rows.Scan(&e.From, &e.Relation, &e.To); err == nil {
			edges = append(edges, e)
		}
	}
	return edges, nil
}

func (s *DoltStore) Walk(ctx context.Context, root, rel string, dir parchment.Direction, maxDepth int, fn parchment.WalkFn) error {
	visited := map[string]bool{root: true}
	queue := []struct {
		id    string
		depth int
	}{{root, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if maxDepth > 0 && cur.depth >= maxDepth {
			continue
		}
		edges, err := s.Neighbors(ctx, cur.id, rel, dir)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if !fn(cur.depth+1, e) {
				return nil
			}
			next := e.To
			if dir == parchment.Incoming {
				next = e.From
			}
			if !visited[next] {
				visited[next] = true
				queue = append(queue, struct {
					id    string
					depth int
				}{next, cur.depth + 1})
			}
		}
	}
	return nil
}

// --- SequenceStore ---

func (s *DoltStore) NextID(ctx context.Context, prefix string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close on read-only query

	var next int64
	row := tx.QueryRowContext(ctx, `SELECT next_val FROM sequences WHERE prefix=? FOR UPDATE`, prefix)
	if err := row.Scan(&next); errors.Is(err, sql.ErrNoRows) {
		next = 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO sequences (prefix, next_val) VALUES (?,2)`, prefix); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE sequences SET next_val=? WHERE prefix=?`, next+1, prefix); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d", prefix, next), nil
}

func (s *DoltStore) SeedSequence(ctx context.Context, prefix string, val uint64, force bool) error {
	if force {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO sequences (prefix, next_val) VALUES (?,?)
			 ON DUPLICATE KEY UPDATE next_val=VALUES(next_val)`,
			prefix, val)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT IGNORE INTO sequences (prefix, next_val) VALUES (?,?)`, prefix, val)
	return err
}

func (s *DoltStore) NextScopedID(ctx context.Context, scopeKey, kindCode string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close on read-only query

	var next int64
	row := tx.QueryRowContext(ctx,
		`SELECT next_val FROM scoped_sequences WHERE scope_key=? AND kind_code=? FOR UPDATE`,
		scopeKey, kindCode)
	if err := row.Scan(&next); errors.Is(err, sql.ErrNoRows) {
		next = 1
		if _, er2 := tx.ExecContext(ctx, `INSERT INTO scoped_sequences (scope_key, kind_code, next_val) VALUES (?,?,2)`, scopeKey, kindCode); er2 != nil {
			return "", er2
		}
	} else if err != nil {
		return "", err
	} else {
		if _, er2 := tx.ExecContext(ctx, `UPDATE scoped_sequences SET next_val=? WHERE scope_key=? AND kind_code=?`, next+1, scopeKey, kindCode); er2 != nil {
			return "", er2
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%d", scopeKey, kindCode, next), nil
}

func (s *DoltStore) NextScopedAlias(ctx context.Context, scopeKey, kindCode string) (string, error) {
	id, err := s.NextScopedID(ctx, scopeKey, kindCode)
	return id, err
}

func (s *DoltStore) NextSeq(ctx context.Context, key string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // best-effort close on read-only query

	var next int64
	row := tx.QueryRowContext(ctx, `SELECT next_val FROM sequences WHERE prefix=? FOR UPDATE`, key)
	if err := row.Scan(&next); errors.Is(err, sql.ErrNoRows) {
		next = 1
		if _, er2 := tx.ExecContext(ctx, `INSERT INTO sequences (prefix, next_val) VALUES (?,2)`, key); er2 != nil {
			return 0, er2
		}
	} else if err != nil {
		return 0, err
	} else {
		if _, er2 := tx.ExecContext(ctx, `UPDATE sequences SET next_val=? WHERE prefix=?`, next+1, key); er2 != nil {
			return 0, er2
		}
	}
	return next, tx.Commit()
}

// --- ScopeStore ---

func (s *DoltStore) GetScopeKey(ctx context.Context, scope string) (scopeKey string, isAutoKey bool, err error) { //nolint:gocritic // named returns match interface signature clarity
	var key string
	var isAuto bool
	err = s.db.QueryRowContext(ctx,
		`SELECT scope_key, is_auto FROM scope_keys WHERE scope=?`, scope).Scan(&key, &isAuto)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return key, isAuto, err
}

func (s *DoltStore) SetScopeKey(ctx context.Context, scope, key string, auto bool) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scope_keys (scope, scope_key, is_auto) VALUES (?,?,?)
		 ON DUPLICATE KEY UPDATE scope_key=VALUES(scope_key), is_auto=VALUES(is_auto)`,
		scope, key, auto)
	return err
}

func (s *DoltStore) ListScopeKeys(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope, scope_key FROM scope_keys`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query
	m := map[string]string{}
	for rows.Next() {
		var scope, key string
		if err := rows.Scan(&scope, &key); err == nil {
			m[scope] = key
		}
	}
	return m, nil
}

func (s *DoltStore) SetScopeLabels(ctx context.Context, scope string, labels []string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO scope_labels (scope, labels) VALUES (?,?)
		 ON DUPLICATE KEY UPDATE labels=VALUES(labels)`,
		scope, joinStr(labels))
	return err
}

func (s *DoltStore) GetScopeLabels(ctx context.Context, scope string) ([]string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT labels FROM scope_labels WHERE scope=?`, scope).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return splitStr(raw), nil
}

func (s *DoltStore) ScopesByLabel(ctx context.Context, label string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT scope FROM scope_labels WHERE labels LIKE ?`, "%"+label+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query
	var scopes []string
	for rows.Next() {
		var sc string
		if err := rows.Scan(&sc); err == nil {
			scopes = append(scopes, sc)
		}
	}
	return scopes, nil
}

func (s *DoltStore) ListScopeInfo(ctx context.Context) ([]parchment.ScopeInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT k.scope, k.scope_key, COALESCE(l.labels,'')
		 FROM scope_keys k LEFT JOIN scope_labels l ON k.scope=l.scope`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query
	var infos []parchment.ScopeInfo
	for rows.Next() {
		var info parchment.ScopeInfo
		var labels string
		if err := rows.Scan(&info.Scope, &info.Key, &labels); err == nil {
			info.Labels = splitStr(labels)
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// --- Embedding store ---

func (s *DoltStore) PutEmbedding(ctx context.Context, artifactID, model string, vec []float32) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO artifact_embeddings (artifact_id, model, vector) VALUES (?,?,?)
		 ON DUPLICATE KEY UPDATE vector=VALUES(vector)`,
		artifactID, model, vecToBlob(vec))
	return err
}

func (s *DoltStore) GetEmbedding(ctx context.Context, artifactID, model string) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT vector FROM artifact_embeddings WHERE artifact_id=? AND model=?`,
		artifactID, model).Scan(&blob)
	if err != nil {
		return nil, err
	}
	return blobToVec(blob), nil
}

func (s *DoltStore) SearchSemantic(ctx context.Context, model string, query []float32, n int) ([]string, error) {
	// O(n) cosine scan — same as SQLiteStore for now.
	// Future: replace with Dolt VECTOR INDEX once Dolt vector support lands in v0.x.
	rows, err := s.db.QueryContext(ctx,
		`SELECT artifact_id, vector FROM artifact_embeddings WHERE model=?`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // best-effort close on read-only query

	type scored struct {
		id    string
		score float32
	}
	var results []scored
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			continue
		}
		sim := parchment.CosineSimilarity(query, blobToVec(blob))
		results = append(results, scored{id, sim})
	}
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
	if n > len(results) {
		n = len(results)
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = results[i].id
	}
	return ids, nil
}

// --- helpers ---

func joinStr(ss []string) string { return strings.Join(ss, ",") }
func splitStr(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func scanArtifact(row *sql.Row) (*parchment.Artifact, error) {
	var art parchment.Artifact
	var sections, links, extra, dependsOn, labels string
	var createdAt, updatedAt, insertedAt string
	err := row.Scan(
		&art.UID, &art.ID, &art.Alias, &art.Kind, &art.Scope, &art.Status,
		&art.Parent, &art.Title, &art.Goal,
		&dependsOn, &labels, &art.Priority, &art.Sprint,
		&sections, &links, &extra,
		&createdAt, &updatedAt, &insertedAt,
	)
	if err != nil {
		return nil, err
	}
	art.DependsOn = splitStr(dependsOn)
	art.Labels = splitStr(labels)
	_ = json.Unmarshal([]byte(sections), &art.Sections)
	_ = json.Unmarshal([]byte(links), &art.Links)
	_ = json.Unmarshal([]byte(extra), &art.Extra)
	art.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	art.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	art.InsertedAt, _ = time.Parse(time.RFC3339, insertedAt)
	return &art, nil
}

func scanArtifacts(rows *sql.Rows) ([]*parchment.Artifact, error) {
	var arts []*parchment.Artifact
	for rows.Next() {
		var art parchment.Artifact
		var sections, links, extra, dependsOn, labels string
		var createdAt, updatedAt, insertedAt string
		if err := rows.Scan(
			&art.UID, &art.ID, &art.Alias, &art.Kind, &art.Scope, &art.Status,
			&art.Parent, &art.Title, &art.Goal,
			&dependsOn, &labels, &art.Priority, &art.Sprint,
			&sections, &links, &extra,
			&createdAt, &updatedAt, &insertedAt,
		); err != nil {
			continue
		}
		art.DependsOn = splitStr(dependsOn)
		art.Labels = splitStr(labels)
		_ = json.Unmarshal([]byte(sections), &art.Sections)
		_ = json.Unmarshal([]byte(links), &art.Links)
		_ = json.Unmarshal([]byte(extra), &art.Extra)
		art.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		art.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		art.InsertedAt, _ = time.Parse(time.RFC3339, insertedAt)
		arts = append(arts, &art)
	}
	return arts, rows.Err()
}

// vecToBlob / blobToVec mirror the SQLiteStore implementation.
func vecToBlob(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		u := float32bits(f)
		b[i*4] = byte(u & 0xFF)           //nolint:gosec // intentional low-byte extraction
		b[i*4+1] = byte((u >> 8) & 0xFF)  //nolint:gosec // intentional byte slice
		b[i*4+2] = byte((u >> 16) & 0xFF) //nolint:gosec // intentional byte slice
		b[i*4+3] = byte((u >> 24) & 0xFF) //nolint:gosec // intentional high-byte extraction
	}
	return b
}

func blobToVec(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		u := uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
		v[i] = float32frombits(u)
	}
	return v
}

func float32bits(f float32) uint32     { return math.Float32bits(f) }
func float32frombits(u uint32) float32 { return math.Float32frombits(u) }

// doltSchema is the MySQL-compatible DDL for all tables.
var doltSchema = []string{
	`CREATE TABLE IF NOT EXISTS artifacts (
		uid         VARCHAR(64)   NOT NULL DEFAULT '',
		id          VARCHAR(255)  NOT NULL,
		alias       VARCHAR(255)  NOT NULL DEFAULT '',
		kind        VARCHAR(64)   NOT NULL DEFAULT '',
		scope       VARCHAR(255)  NOT NULL DEFAULT '',
		status      VARCHAR(64)   NOT NULL DEFAULT '',
		parent      VARCHAR(255)  NOT NULL DEFAULT '',
		title       LONGTEXT      NOT NULL,
		goal        LONGTEXT      NOT NULL DEFAULT '',
		depends_on  TEXT          NOT NULL DEFAULT '',
		labels      TEXT          NOT NULL DEFAULT '',
		priority    VARCHAR(64)   NOT NULL DEFAULT '',
		sprint      VARCHAR(255)  NOT NULL DEFAULT '',
		sections    LONGTEXT      NOT NULL DEFAULT '[]',
		links       LONGTEXT      NOT NULL DEFAULT '{}',
		extra       LONGTEXT      NOT NULL DEFAULT '{}',
		created_at  VARCHAR(64)   NOT NULL DEFAULT '',
		updated_at  VARCHAR(64)   NOT NULL DEFAULT '',
		inserted_at VARCHAR(64)   NOT NULL DEFAULT '',
		PRIMARY KEY (id)
	)`,
	`CREATE TABLE IF NOT EXISTS edges (
		from_id  VARCHAR(255) NOT NULL,
		relation VARCHAR(64)  NOT NULL,
		to_id    VARCHAR(255) NOT NULL,
		PRIMARY KEY (from_id, relation, to_id)
	)`,
	`CREATE TABLE IF NOT EXISTS sequences (
		prefix   VARCHAR(255) NOT NULL,
		next_val BIGINT       NOT NULL DEFAULT 1,
		PRIMARY KEY (prefix)
	)`,
	`CREATE TABLE IF NOT EXISTS scoped_sequences (
		scope_key VARCHAR(255) NOT NULL,
		kind_code VARCHAR(255) NOT NULL,
		next_val  BIGINT       NOT NULL DEFAULT 1,
		PRIMARY KEY (scope_key, kind_code)
	)`,
	`CREATE TABLE IF NOT EXISTS scope_keys (
		scope     VARCHAR(255) NOT NULL,
		scope_key VARCHAR(64)  NOT NULL,
		is_auto   TINYINT(1)   NOT NULL DEFAULT 0,
		PRIMARY KEY (scope)
	)`,
	`CREATE TABLE IF NOT EXISTS scope_labels (
		scope  VARCHAR(255) NOT NULL,
		labels TEXT         NOT NULL DEFAULT '',
		PRIMARY KEY (scope)
	)`,
	`CREATE TABLE IF NOT EXISTS artifact_embeddings (
		artifact_id VARCHAR(255) NOT NULL,
		model       VARCHAR(128) NOT NULL,
		vector      LONGBLOB     NOT NULL,
		PRIMARY KEY (artifact_id, model)
	)`,
}

// Compile-time: DoltStore must satisfy parchment.Store.
var _ parchment.Store = (*DoltStore)(nil)
