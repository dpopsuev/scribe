package workspace

import (
	"fmt"
	"time"
)

// TimeDetector produces temporal context labels from the current time.
// It emits two labels at different granularities:
//
//	time:2026.q2   — quarter (strategic: "what did we do in Q2?")
//	time:2026.w24  — ISO week (tactical: "what was I working on this week?")
//
// The time source comes from WorkspaceInputs.Now so tests can inject a
// fixed timestamp. When Now is zero, time.Now() is used.
type TimeDetector struct{}

func (TimeDetector) Detect(inputs WorkspaceInputs) []string {
	now := inputs.Now
	if now.IsZero() {
		now = time.Now()
	}
	year, week := now.ISOWeek()
	quarter := (int(now.Month())-1)/3 + 1
	return []string{
		fmt.Sprintf("time:%d.q%d", now.Year(), quarter),
		fmt.Sprintf("time:%d.w%02d", year, week),
	}
}
