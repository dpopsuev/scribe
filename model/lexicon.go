package model

// Artifact statuses.
const (
	StatusDraft     = "draft"
	StatusActive    = "active"
	StatusCurrent   = "current"
	StatusOpen      = "open"
	StatusComplete  = "complete"
	StatusCancelled = "cancelled"
	StatusDismissed = "dismissed"
	StatusPromoted  = "promoted"
	StatusRetired   = "retired"
	StatusArchived  = "archived"
	StatusAccepted  = "accepted"
)

// Artifact kinds.
const (
	KindTask       = "task"
	KindSpec       = "spec"
	KindBug        = "bug"
	KindGoal       = "goal"
	KindCampaign   = "campaign"
	KindNeed       = "need"
	KindDoc        = "doc"
	KindRef        = "ref"
	KindTemplate   = "template"
	KindDecision     = "decision"
	KindConfig       = "config"
	KindMirror       = "mirror"
	KindSecurityCase = "security_case"
)

// Artifact field names (for SetField, update, etc.).
const (
	FieldStatus    = "status"
	FieldTitle     = "title"
	FieldGoal      = "goal"
	FieldScope     = "scope"
	FieldParent    = "parent"
	FieldPriority  = "priority"
	FieldSprint    = "sprint"
	FieldKind      = "kind"
	FieldDependsOn = "depends_on"
	FieldLabels    = "labels"
)

// Graph traversal directions.
const (
	DirOutbound = "outbound"
	DirInbound  = "inbound"
	DirBoth     = "both"
	DirOutgoing = "outgoing"
	DirIncoming = "incoming"
)
