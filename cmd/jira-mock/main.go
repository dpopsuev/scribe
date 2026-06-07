// jira-mock streams synthetic Jira ticket NDJSON to stdout.
// Pipe it into `scribe ingest` or curl to populate the graph with realistic issue data.
//
// Usage:
//
//	jira-mock | curl -s -X POST http://localhost:8083/api/v1/ingest \
//	    -H 'Content-Type: application/x-ndjson' --data-binary @- | jq
//
//	jira-mock --tickets 50 --project MYPROJ
//
//nolint:gosec,funlen,goconst,gocritic // mock tool — weak rand, repeated literals, and unnamed returns are fine for synthetic data generation
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
	adjectives = []string{
		"null pointer", "race condition", "memory leak", "off-by-one", "deadlock",
		"integer overflow", "timeout", "authentication", "authorization", "pagination",
		"encoding", "serialization", "connection pool", "cache invalidation", "rate limit",
	}
	areas = []string{
		"in graph rendering", "in API gateway", "in session management", "in data pipeline",
		"in search indexer", "in notification service", "in billing module", "in auth flow",
		"in export handler", "in webhook processor",
	}
	storyTitles = []string{
		"Add streaming NDJSON support to ingest endpoint",
		"Implement cursor-based pagination for artifact list",
		"Expose graph metrics via Prometheus endpoint",
		"Add dark mode support to web UI",
		"Support multi-tenant scoping in rule engine",
		"Integrate GitHub PR events into ingest pipeline",
		"Build saved-query bucket UI",
		"Add WebSocket push for live graph updates",
		"Implement artifact archival policy enforcement",
		"Add bulk artifact status transition API",
	}
	taskTitles = []string{
		"Update Go toolchain to 1.26",
		"Migrate parchment store to WAL mode",
		"Add integration tests for ingest handler",
		"Clean up deprecated API endpoints",
		"Rotate internal mTLS certificates",
		"Document NDJSON wire format",
		"Benchmark graph traversal under 10k nodes",
		"Remove unused locus coupling metrics",
		"Pin three.js CDN version in graph template",
		"Prune stale feature flags",
	}
	assignees = []string{"alice", "bob", "carol", "dave", "eve", "frank", "grace", "hank"}
	reporters = []string{"pm-alice", "pm-bob", "tech-lead-carol", "em-dave"}
)

func pickPriority() (string, string) {
	priorities := []string{"high", "medium", "low"}
	jiraPriorities := []string{"High", "Medium", "Low"}
	n := rand.IntN(len(priorities))
	return priorities[n], jiraPriorities[n]
}

func pickIssueType() (string, string) {
	n := rand.IntN(3)
	switch n {
	case 0:
		return "bug", "Bug"
	case 1:
		return "story", "Story"
	default:
		return "task", "Task"
	}
}

func pickStatus() string {
	statuses := []string{"Open", "In Progress", "Done"}
	return statuses[rand.IntN(len(statuses))]
}

func bugTitle() string {
	return fmt.Sprintf("Fix %s %s", adjectives[rand.IntN(len(adjectives))], areas[rand.IntN(len(areas))])
}

func titleFor(issueType string) string {
	switch issueType {
	case "bug":
		return bugTitle()
	case "story":
		return storyTitles[rand.IntN(len(storyTitles))]
	default:
		return taskTitles[rand.IntN(len(taskTitles))]
	}
}

func descriptionFor(issueType, title string) string {
	switch issueType {
	case "bug":
		return fmt.Sprintf(
			"## Bug Report\n\n**Summary**: %s\n\n**Steps to reproduce**:\n1. Trigger the condition\n2. Observe the failure\n\n**Expected**: system handles it gracefully\n**Actual**: panic / incorrect result",
			title,
		)
	case "story":
		return fmt.Sprintf(
			"## User Story\n\nAs a developer, I want to %s so that the system is more reliable and maintainable.\n\n**Background**: This was identified during the last sprint retrospective.",
			title,
		)
	default:
		return fmt.Sprintf(
			"## Task\n\n%s\n\nThis is a maintenance task. No user-visible change expected.",
			title,
		)
	}
}

func acceptanceCriteriaFor(issueType string) string {
	switch issueType {
	case "bug":
		return "- [ ] Reproducer test added\n- [ ] Fix merged and deployed\n- [ ] No regression in CI"
	case "story":
		return "- [ ] Feature implemented behind feature flag\n- [ ] Unit tests cover happy and error paths\n- [ ] Documentation updated"
	default:
		return "- [ ] Task completed\n- [ ] Verified in staging\n- [ ] Ticket closed"
	}
}

func main() { //nolint:funlen,gosec,staticcheck // mock tool: length intentional; weak rand fine for synthetic data
	ticketCount := flag.Int("tickets", 10, "number of Jira tickets to generate")
	source := flag.String("source", "jira", "source label value")
	project := flag.String("project", "PROJ", "Jira project key")
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

	ticketIDs := make([]string, *ticketCount)

	for i := range *ticketCount {
		n := i + 1
		id := fmt.Sprintf("jira:%s-%d", *project, n)
		ticketIDs[i] = id

		labelPriority, jiraPriority := pickPriority()
		issueType, jiraIssueType := pickIssueType()
		jiraStatus := pickStatus()
		title := titleFor(issueType)
		assignee := assignees[rand.IntN(len(assignees))]
		reporter := reporters[rand.IntN(len(reporters))]
		created := now.Add(-time.Duration(rand.IntN(90*24)) * time.Hour)
		updated := created.Add(time.Duration(rand.IntN(48)) * time.Hour)
		storyPoints := 0
		if issueType == "story" {
			storyPoints = []int{1, 2, 3, 5, 8, 13}[rand.IntN(6)]
		}

		emit(map[string]any{
			"type":   "node",
			"id":     id,
			"kind":   "note",
			"title":  title,
			"status": "active",
			"labels": []string{
				"source:" + *source,
				"kind:issue",
				"priority:" + labelPriority,
				"type:" + issueType,
			},
			"extra": map[string]any{
				"summary":      title,
				"status":       jiraStatus,
				"priority":     jiraPriority,
				"assignee":     assignee,
				"reporter":     reporter,
				"created":      created.Format(time.RFC3339),
				"updated":      updated.Format(time.RFC3339),
				"issue_type":   jiraIssueType,
				"story_points": storyPoints,
			},
			"sections": []map[string]any{
				{"name": "description", "text": descriptionFor(issueType, title)},
				{"name": "acceptance_criteria", "text": acceptanceCriteriaFor(issueType)},
			},
		})
		totalNodes++
	}

	// Blocking edges: roughly 20% of tickets block another
	for i, from := range ticketIDs {
		if rand.IntN(5) != 0 {
			continue
		}
		to := ticketIDs[rand.IntN(len(ticketIDs))]
		if to == from {
			continue
		}
		emit(map[string]any{
			"type":     "edge",
			"from":     from,
			"to":       to,
			"relation": "blocks",
		})
		_ = i
		totalEdges++
	}

	// Related edges: roughly 30% of tickets relate to another
	for _, from := range ticketIDs {
		if rand.IntN(10) >= 3 {
			continue
		}
		to := ticketIDs[rand.IntN(len(ticketIDs))]
		if to == from {
			continue
		}
		emit(map[string]any{
			"type":     "edge",
			"from":     from,
			"to":       to,
			"relation": "relates_to",
		})
		totalEdges++
	}

	emit(map[string]any{
		"type":         "meta",
		"source":       *source,
		"project":      *project,
		"generated_at": now.Format(time.RFC3339),
		"total_nodes":  totalNodes,
		"total_edges":  totalEdges,
	})

	fmt.Fprintf(os.Stderr, "streamed %d nodes, %d edges\n", totalNodes, totalEdges)
}
