// github-mock streams synthetic GitHub PR NDJSON to stdout.
// Pipe it into `scribe ingest` or curl to populate the graph with realistic PR data.
//
// Usage:
//
//	github-mock | curl -s -X POST http://localhost:8083/api/v1/ingest \
//	    -H 'Content-Type: application/x-ndjson' --data-binary @- | jq
//
//	github-mock --prs 20 --repo myorg/myrepo
//
//nolint:gosec,funlen,goconst // mock tool — weak rand and repeated literals are fine for synthetic data generation
//nolint:gosec,funlen,staticcheck // mock/test tool — weak rand and length are intentional
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"time"
)

var (
	prTitles = []string{
		"feat: add streaming NDJSON support",
		"fix: resolve null pointer in graph rendering",
		"refactor: extract renderer into separate module",
		"feat: implement cursor-based pagination",
		"fix: race condition in session manager",
		"chore: update Go toolchain to 1.26",
		"feat: expose Prometheus metrics endpoint",
		"fix: memory leak in connection pool",
		"test: add integration tests for ingest handler",
		"feat: add WebSocket push for live graph updates",
		"fix: off-by-one in pagination slice",
		"refactor: replace cobra with stdlib flag",
		"feat: implement artifact archival policy",
		"fix: authentication bypass in API gateway",
		"docs: document NDJSON wire format",
		"feat: add bulk artifact status transition API",
		"fix: deadlock in rule engine evaluation",
		"chore: pin three.js CDN version",
		"feat: support multi-tenant scoping",
		"fix: timeout in graph traversal under load",
	}

	authors = []string{
		"alice-dev", "bob-eng", "carol-sre", "dave-backend",
		"eve-frontend", "frank-infra", "grace-ml", "hank-platform",
	}

	baseBranches = []string{"main", "main", "main", "develop", "release/v2"}
)

func pickState() string {
	n := rand.IntN(10)
	switch {
	case n < 5:
		return "merged"
	case n < 8:
		return "open"
	default:
		return "closed"
	}
}

func prDescription(title string) string {
	return fmt.Sprintf(
		"## Summary\n\n%s\n\n## Changes\n\n- Core logic updated\n- Tests added / updated\n- Documentation revised where applicable\n\n## Testing\n\nRan `go test ./...` and Playwright suite locally. All green.",
		title,
	)
}

func reviewComments(state string) string {
	switch state {
	case "merged":
		return "LGTM. Approved by two reviewers. Merged after CI passed."
	case "closed":
		return "Closed without merging — superseded by a different approach."
	default:
		return "Review in progress. Awaiting approval from at least one codeowner."
	}
}

func headBranch(title string, number int) string {
	prefix := "feat"
	if len(title) > 4 && title[0:4] == "fix:" {
		prefix = "fix"
	} else if len(title) > 7 && title[0:7] == "chore: " {
		prefix = "chore"
	}
	return fmt.Sprintf("%s/pr-%d", prefix, number)
}

func main() { //nolint:funlen,gosec,staticcheck // mock tool: length intentional; weak rand fine for synthetic data
	prCount := flag.Int("prs", 5, "number of GitHub PRs to generate")
	repo := flag.String("repo", "octocat/hello-world", "GitHub repository (owner/repo)")
	source := flag.String("source", "github", "source label value")
	jiraProject := flag.String("jira-project", "PROJ", "Jira project key for fixes edges")
	jiraTickets := flag.Int("jira-tickets", 10, "number of Jira tickets available for fixes edges")
	flag.Parse()

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)

	emit := func(v any) {
		if err := enc.Encode(v); err != nil {
			fmt.Fprintf(os.Stderr, "encode error: %v\n", err)
			os.Exit(1)
		}
	}

	now := time.Now().UTC()
	totalNodes := 0
	totalEdges := 0

	for i := range *prCount {
		number := i + 1
		id := fmt.Sprintf("github:%s#%d", *repo, number)
		title := prTitles[rand.IntN(len(prTitles))]
		state := pickState()
		author := authors[rand.IntN(len(authors))]
		base := baseBranches[rand.IntN(len(baseBranches))]
		head := headBranch(title, number)
		additions := rand.IntN(400) + 1
		deletions := rand.IntN(200)
		commits := rand.IntN(8) + 1
		created := now.Add(-time.Duration(rand.IntN(60*24)) * time.Hour)

		labels := []string{
			"source:" + *source,
			"kind:pr",
			"state:" + state,
			"draft:false",
		}

		extra := map[string]any{
			"repo":        *repo,
			"number":      number,
			"author":      author,
			"base_branch": base,
			"head_branch": head,
			"additions":   additions,
			"deletions":   deletions,
			"commits":     commits,
			"state":       state,
			"created_at":  created.Format(time.RFC3339),
		}
		if state == "merged" {
			mergedAt := created.Add(time.Duration(rand.IntN(72)) * time.Hour)
			extra["merged_at"] = mergedAt.Format(time.RFC3339)
		}

		emit(map[string]any{
			"type":   "node",
			"id":     id,
			"kind":   "note",
			"title":  title,
			"status": "active",
			"labels": labels,
			"extra":  extra,
			"sections": []map[string]any{
				{"name": "description", "text": prDescription(title)},
				{"name": "review_comments", "text": reviewComments(state)},
			},
		})
		totalNodes++

		// fixes edge: ~40% of PRs reference a Jira ticket
		if *jiraTickets > 0 && rand.IntN(10) < 4 {
			jiraID := fmt.Sprintf("jira:%s-%d", *jiraProject, rand.IntN(*jiraTickets)+1)
			emit(map[string]any{
				"type":     "edge",
				"from":     id,
				"to":       jiraID,
				"relation": "fixes",
			})
			totalEdges++
		}

		// produced_by edge: PR produced by its author
		authorID := fmt.Sprintf("github:user:%s", author)
		emit(map[string]any{
			"type":     "edge",
			"from":     id,
			"to":       authorID,
			"relation": "produced_by",
		})
		totalEdges++
	}

	emit(map[string]any{
		"type":         "meta",
		"source":       *source,
		"repo":         *repo,
		"generated_at": now.Format(time.RFC3339),
		"total_nodes":  totalNodes,
		"total_edges":  totalEdges,
	})

	fmt.Fprintf(os.Stderr, "streamed %d nodes, %d edges\n", totalNodes, totalEdges)
}
