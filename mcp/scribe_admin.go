package mcp

// scribe_admin.go — thin shim re-exporting service types and wrapping service methods.
// Business logic has moved to scribe/service. This file keeps backward compatibility
// with cmd/scribe until SCR-TSK-347 migrates the CLI to use service directly.

import (
	"context"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

// Type aliases — mcp consumers (cmd/scribe) continue using mcp.SetGoalInput etc.
// while service owns the definitions.
type MotdResult = service.MotdResult
type DashboardScope = service.DashboardScope
type DashboardResult = service.DashboardResult
type InventoryResult = service.InventoryResult
type SetGoalInput = service.SetGoalInput
type SetGoalResult = service.SetGoalResult
type DrainEntry = service.DrainEntry

// Motd returns the message of the day. Delegates to service.
func Motd(ctx context.Context, proto *parchment.Protocol) (*MotdResult, error) {
	svc := service.New(proto, nil, nil)
	return svc.Motd(ctx)
}

// Dashboard returns storage and staleness statistics. Delegates to service.
func Dashboard(ctx context.Context, proto *parchment.Protocol, staleDays int) (*DashboardResult, error) {
	svc := service.New(proto, nil, nil)
	return svc.Dashboard(ctx, staleDays)
}

// Inventory returns a count-by-kind/status summary. Delegates to service.
func Inventory(ctx context.Context, proto *parchment.Protocol) (*InventoryResult, error) {
	svc := service.New(proto, nil, nil)
	return svc.Inventory(ctx)
}

// SetGoal archives existing active goals and creates a new one. Delegates to service.
func SetGoal(ctx context.Context, proto *parchment.Protocol, in SetGoalInput) (*SetGoalResult, error) {
	svc := service.New(proto, nil, nil)
	return svc.SetGoal(ctx, in)
}

// DrainDiscover lists legacy .md files under path. Delegates to service.
func DrainDiscover(ctx context.Context, path string) ([]DrainEntry, error) {
	svc := service.New(nil, nil, nil)
	return svc.DrainDiscover(ctx, path)
}

// DrainCleanup removes all drain-candidate files under path. Delegates to service.
func DrainCleanup(ctx context.Context, path string) (int, error) {
	svc := service.New(nil, nil, nil)
	return svc.DrainCleanup(ctx, path)
}

// IsComponentLabel reports whether s matches the Scribe component label format.
func IsComponentLabel(s string) bool {
	return service.IsComponentLabel(s)
}
