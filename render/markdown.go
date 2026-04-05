package render

import parchment "github.com/dpopsuev/scribe/internal/parchment"

// Markdown renders an artifact as a human-readable markdown document.
var Markdown = parchment.RenderMarkdown

// Table renders a list of artifacts as an aligned text table.
var Table = parchment.RenderTable

// GroupedTable renders artifacts grouped by a field.
var GroupedTable = parchment.RenderGroupedTable

// GroupedTableByScopeLabel groups artifacts by scope labels.
var GroupedTableByScopeLabel = parchment.RenderGroupedTableByScopeLabel

// JSON renders an artifact as JSON.
var JSON = parchment.RenderJSON

// JSONList renders a list of artifacts as a JSON array.
var JSONList = parchment.RenderJSONList
