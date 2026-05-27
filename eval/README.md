# Retrieval Evaluation Harness

A/B test: FTS5 (System A) vs cosine similarity (System B) for agent memory retrieval.

## What we're measuring

**Precision@5** — of the 5 results returned for each query, how many are actually relevant?
Evaluated manually: 1 = relevant, 0 = not relevant.

Overall score = mean precision@5 across all 20 queries.

## Systems under test

| System | Backend | Search |
|--------|---------|--------|
| A | SQLite | FTS5 keyword matching (current) |
| B | SQLite | cosine similarity on embeddings (Phase 2) |
| C | Dolt | FULLTEXT index |
| D | Dolt | VECTOR INDEX + embeddings |

## Corpus

140 compaction summaries from Claude Code and Pi sessions across:
- parchment, scribe, djinn, tako, origami, alef, locus, oculus, hegemony, troupe, emcee

## Running the evaluation

```bash
# Step 1: ingest corpus into knowledge store
scribe eval ingest --sessions ~/.claude/projects/

# Step 2: run queries, review results
scribe eval run --queries eval/queries.yaml --system A

# Step 3: score results interactively
scribe eval score --results eval/results/latest.json

# Step 4: report
scribe eval report --results eval/results/
```

## Query design

20 queries across 4 bands:

- **Easy (Q01-Q05)**: verbatim keywords in corpus — FTS5 baseline
- **Medium (Q06-Q10)**: partial overlap — both systems should find
- **Hard (Q11-Q15)**: semantic only — cosine should win here
- **Cross (Q16-Q20)**: breadth across all projects

FTS5 should score well on easy/medium. If cosine beats FTS5 on hard
while matching it on easy/medium, vector search is worth the investment.
