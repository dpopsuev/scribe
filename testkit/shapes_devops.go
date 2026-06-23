//nolint:gosec,goconst // weak rand throughout — synthetic test data; kind/status literals are vocabulary data
package testkit

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/dpopsuev/scribe/internal/ingest"
)

// GitCommitShape produces a node shaped like a git commit with ticket references.
var GitCommitShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	prefixes := []string{"feat", "fix", "chore", "test", "refactor"}
	components := []string{"daemon", "operator", "proxy", "api", "config"}
	hash := fmt.Sprintf("%08x%08x%08x%08x%08x", rand.Uint32(), rand.Uint32(), rand.Uint32(), rand.Uint32(), rand.Uint32())
	ticketID := fmt.Sprintf("PROJ-%d", rand.IntN(500)+1)
	subject := fmt.Sprintf("%s(%s): synthetic change %d [%s]", prefixes[rand.IntN(len(prefixes))], components[rand.IntN(len(components))], i, ticketID)
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:commit:%s", source, hash[:12]),
		Kind:   "delivery.commit",
		Title:  subject,
		Status: "active",
		Labels: []string{"source:" + source, "kind:commit", "project:ptp"},
		Extra: map[string]any{
			"commit_hash":   hash,
			"commit_author": "dev" + fmt.Sprint(rand.IntN(5)),
			"ticket_refs":   []string{ticketID},
			"scan_sha":      sha,
		},
	}
}

// CIBuildShape produces a node shaped like a Jenkins/GitHub Actions CI build.
var CIBuildShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	results := []string{"SUCCESS", "FAILURE", "UNSTABLE", "ABORTED"}
	jobs := []string{"far-edge-vran-deployment", "ptp-operator-ci", "linuxptp-daemon-unit", "cloud-event-proxy-e2e"}
	result := results[rand.IntN(len(results))]
	job := jobs[rand.IntN(len(jobs))]
	duration := rand.IntN(3600) + 120
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:ci:%s:%d", source, job, i+1),
		Kind:   "delivery.build",
		Title:  fmt.Sprintf("%s #%d — %s", job, i+1, result),
		Status: "active",
		Labels: []string{"source:" + source, "kind:ci-build", "result:" + result, "project:ptp"},
		Extra: map[string]any{
			"ci_backend":   "jenkins",
			"ci_job":       job,
			"ci_run_id":    i + 1,
			"ci_result":    result,
			"duration_sec": duration,
			"scan_sha":     sha,
		},
	}
}

// TestResultShape produces a node shaped like a test item from Report Portal or CI.
var TestResultShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	statuses := []string{"PASSED", "FAILED", "SKIPPED"}
	suites := []string{"ptp-functional", "clock-sync-e2e", "holdover-regression", "offset-parsing-unit"}
	status := statuses[rand.IntN(len(statuses))]
	suite := suites[rand.IntN(len(suites))]
	duration := rand.IntN(300) + 1
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:test:%s:case-%d", source, suite, i),
		Kind:   "test.result",
		Title:  fmt.Sprintf("[%s] %s/test_case_%d — %s", suite, suite, i, status),
		Status: "active",
		Labels: []string{"source:" + source, "kind:test-result", "test-status:" + status, "project:ptp"},
		Extra: map[string]any{
			"test_suite":   suite,
			"test_status":  status,
			"duration_sec": duration,
			"defect_type":  defectFor(status),
			"scan_sha":     sha,
		},
	}
}

// ReleaseShape produces a node shaped like a versioned software release.
var ReleaseShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	major := 4 + rand.IntN(2)
	minor := rand.IntN(20)
	version := fmt.Sprintf("%d.%d.%d", major, minor, i)
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:release:v%s", source, version),
		Kind:   "delivery.release",
		Title:  fmt.Sprintf("Release v%s", version),
		Status: "active",
		Labels: []string{"source:" + source, "kind:release", "version:" + version, "project:ptp"},
		Extra: map[string]any{
			"version":         version,
			"artifacts_count": rand.IntN(10) + 1,
			"changelog_url":   fmt.Sprintf("https://github.com/org/repo/releases/tag/v%s", version),
			"scan_sha":        sha,
		},
	}
}

// DeploymentShape produces a node shaped like a K8s deployment event.
var DeploymentShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	envs := []string{"staging", "production", "lab", "canary"}
	statuses := []string{"Progressing", "Available", "Failed"}
	env := envs[rand.IntN(len(envs))]
	depStatus := statuses[rand.IntN(len(statuses))]
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:deploy:%s:%d", source, env, i),
		Kind:   "delivery.deployment",
		Title:  fmt.Sprintf("Deploy #%d to %s — %s", i, env, depStatus),
		Status: "active",
		Labels: []string{"source:" + source, "kind:deployment", "environment:" + env, "project:ptp"},
		Extra: map[string]any{
			"environment": env,
			"image":       fmt.Sprintf("ghcr.io/org/ptp-operator:v4.16.%d", i),
			"replicas":    rand.IntN(3) + 1,
			"dep_status":  depStatus,
			"scan_sha":    sha,
		},
	}
}

// OperationalEventShape produces a node shaped like a runtime operational event (PTP sync, DPLL lock, etc.).
var OperationalEventShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	severities := []string{"info", "warning", "error", "critical"}
	services := []string{"ptp4l", "phc2sys", "cloud-event-proxy", "dpll-netlink"}
	messages := []string{
		"clock state changed to LOCKED",
		"ANNOUNCE_RECEIPT_TIMEOUT_EXPIRES",
		"offset exceeds threshold: 85245ns",
		"DPLL lock acquired on GNSS source",
		"SyncE frequency locked to recovered clock",
		"holdover entered — upstream master lost",
	}
	sev := severities[rand.IntN(len(severities))]
	svc := services[rand.IntN(len(services))]
	msg := messages[rand.IntN(len(messages))]
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:event:%s:%d", source, svc, i),
		Kind:   "delivery.event",
		Title:  fmt.Sprintf("[%s] %s: %s", sev, svc, msg),
		Status: "active",
		Labels: []string{"source:" + source, "kind:operational-event", "severity:" + sev, "service:" + svc, "project:ptp"},
		Extra: map[string]any{
			"severity":  sev,
			"service":   svc,
			"message":   msg,
			"timestamp": time.Now().Add(-time.Duration(rand.IntN(3600)) * time.Second).UTC().Format(time.RFC3339),
			"scan_sha":  sha,
		},
	}
}

// AlertShape produces a node shaped like a Prometheus/Grafana alert.
var AlertShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	metrics := []string{"ptp_clock_offset_ns", "ptp_sync_state", "dpll_lock_status", "phc2sys_delay_ns"}
	states := []string{"firing", "resolved", "pending"}
	metric := metrics[rand.IntN(len(metrics))]
	state := states[rand.IntN(len(states))]
	threshold := (rand.IntN(10) + 1) * 100
	value := rand.IntN(threshold * 3)
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:alert:%s:%d", source, metric, i),
		Kind:   "delivery.alert",
		Title:  fmt.Sprintf("Alert: %s = %d (threshold %d) [%s]", metric, value, threshold, state),
		Status: "active",
		Labels: []string{"source:" + source, "kind:alert", "alert-state:" + state, "project:ptp"},
		Extra: map[string]any{
			"metric":    metric,
			"threshold": threshold,
			"value":     value,
			"state":     state,
			"scan_sha":  sha,
		},
	}
}

func defectFor(status string) string {
	if status == "FAILED" {
		defects := []string{"product_bug", "automation_bug", "system_issue", "to_investigate"}
		return defects[rand.IntN(len(defects))]
	}
	return ""
}

// DevOpsLoopGenerator creates a full DevOps loop dataset — all 8 phases with cross-phase edges.
type DevOpsLoopGenerator struct {
	Project string
	Source  string
	ScanSHA string
}

// DevOpsPhase identifies a phase in the loop for edge generation.
type DevOpsPhase struct {
	Name  string
	Shape ShapeFunc
	Count int
}

// DefaultPhases returns the standard 8-phase DevOps loop with counts per phase.
func DefaultPhases() []DevOpsPhase {
	return []DevOpsPhase{
		{Name: "plan", Shape: JiraIssueShape, Count: 5},
		{Name: "code", Shape: LocusComponentShape, Count: 8},
		{Name: "commit", Shape: GitCommitShape, Count: 10},
		{Name: "build", Shape: CIBuildShape, Count: 3},
		{Name: "test", Shape: TestResultShape, Count: 12},
		{Name: "release", Shape: ReleaseShape, Count: 2},
		{Name: "deploy", Shape: DeploymentShape, Count: 3},
		{Name: "operate", Shape: OperationalEventShape, Count: 6},
		{Name: "monitor", Shape: AlertShape, Count: 4},
	}
}

// Generate produces all nodes and cross-phase edges for the DevOps loop.
func (g *DevOpsLoopGenerator) Generate() ([]ingest.NodeRecord, []ingest.EdgeRecord) {
	sha := g.ScanSHA
	if sha == "" {
		sha = fmt.Sprintf("%x", rand.Uint64())
	}
	source := g.Source
	if source == "" {
		source = "devops-sim"
	}

	phases := DefaultPhases()
	phaseNodes := make(map[string][]string)
	var allNodes []ingest.NodeRecord

	for _, phase := range phases {
		var ids []string
		for i := range phase.Count {
			node := phase.Shape(i, source, sha)
			node.Labels = append(node.Labels, "phase:"+phase.Name)
			if g.Project != "" {
				node.Labels = append(node.Labels, "project:"+g.Project)
			}
			allNodes = append(allNodes, node)
			ids = append(ids, node.ID)
		}
		phaseNodes[phase.Name] = ids
	}

	edges := make([]ingest.EdgeRecord, 0, len(allNodes))

	edges = append(edges, crossPhaseEdges(phaseNodes["commit"], phaseNodes["plan"], "implements")...)
	edges = append(edges, crossPhaseEdges(phaseNodes["code"], phaseNodes["commit"], "traces_to")...)
	edges = append(edges, crossPhaseEdges(phaseNodes["build"], phaseNodes["commit"], "traces_to")...)
	edges = append(edges, crossPhaseEdges(phaseNodes["test"], phaseNodes["build"], "tested_by")...)
	edges = append(edges, crossPhaseEdges(phaseNodes["release"], phaseNodes["build"], "produced_by")...)
	edges = append(edges, crossPhaseEdges(phaseNodes["deploy"], phaseNodes["release"], "traces_to")...)
	edges = append(edges, crossPhaseEdges(phaseNodes["operate"], phaseNodes["deploy"], "traces_to")...)
	edges = append(edges, crossPhaseEdges(phaseNodes["monitor"], phaseNodes["operate"], "relates_to")...)

	return allNodes, edges
}

func crossPhaseEdges(fromIDs, toIDs []string, relation string) []ingest.EdgeRecord {
	if len(toIDs) == 0 {
		return nil
	}
	var edges []ingest.EdgeRecord
	for _, from := range fromIDs {
		to := toIDs[rand.IntN(len(toIDs))]
		edges = append(edges, ingest.EdgeRecord{
			Type:     "edge",
			From:     from,
			To:       to,
			Relation: relation,
		})
	}
	return edges
}
