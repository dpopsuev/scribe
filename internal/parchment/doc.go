// Package parchment provides the domain model, business protocol, and
// reference storage implementation for structured work graph artifacts.
//
// Parchment is the algorithm core of Scribe — it contains all domain
// logic with zero dependency on transport packages (MCP SDK, cobra,
// net/http). Any consumer can import parchment to create, query, and
// manage artifacts with DAG support, lifecycle management, and
// full-text search.
//
// This package will eventually be extracted as a standalone module
// (github.com/dpopsuev/parchment).
package parchment
