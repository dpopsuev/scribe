// Package service contains Scribe's business logic — the shared layer
// consumed by both the MCP handlers and the CLI commands.
//
// Neither surface should call parchment.Protocol directly; all domain
// operations go through Service. Formatting and marshaling belong to
// the caller (mcp package for JSON/LLM output, cmd/scribe for terminal).
package service

import (
	"context"

	parchment "github.com/dpopsuev/parchment"
)

// Service wraps parchment.Protocol and provides all Scribe domain operations.
// It is the single implementation shared by the MCP handlers and the CLI.
type Service struct {
	Proto       *parchment.Protocol
	Snapshotter *parchment.Snapshotter
	HomeScopes  []string
	ReadLog     map[string]bool
	SessionID   string
}

// New constructs a Service.
func New(proto *parchment.Protocol, snapshotter *parchment.Snapshotter, homeScopes []string) *Service {
	return &Service{
		Proto:       proto,
		Snapshotter: snapshotter,
		HomeScopes:  homeScopes,
		ReadLog:     make(map[string]bool),
		SessionID:   NewSessionID(),
	}
}

// RecordRead marks an artifact as read and asynchronously persists the log.
func (s *Service) RecordRead(ctx context.Context, id string) {
	s.ReadLog[id] = true
	go s.PersistReadLog(context.Background(), s.SessionID, s.ReadLog) //nolint:contextcheck,gosec // background intentional
}
