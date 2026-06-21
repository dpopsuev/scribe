package service

import (
	"context"
	"encoding/json"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opAutoRepair)
}

type autoRepairInput struct {
	Scope  string `json:"scope,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

type repairFailure struct {
	Finding HygieneFinding `json:"finding"`
	Error   string         `json:"error"`
}

type autoRepairOutput struct {
	Fixed   []HygieneFinding `json:"fixed"`
	Skipped []HygieneFinding `json:"skipped"`
	Failed  []repairFailure  `json:"failed,omitempty"`
}

var opAutoRepair = Op{
	Name: "auto_repair",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in autoRepairInput
		_ = json.Unmarshal(raw, &in)

		findings := collectFindings(ctx, svc, in.Scope, false)

		out := autoRepairOutput{
			Fixed:   []HygieneFinding{},
			Skipped: []HygieneFinding{},
		}

		for _, f := range findings {
			if !f.SafeAutofix || f.SuggestedFix == nil {
				out.Skipped = append(out.Skipped, f)
				continue
			}

			if in.DryRun {
				out.Fixed = append(out.Fixed, f)
				continue
			}

			if err := applyFix(ctx, svc, f.SuggestedFix); err != nil {
				out.Failed = append(out.Failed, repairFailure{Finding: f, Error: err.Error()})
				continue
			}
			out.Fixed = append(out.Fixed, f)
		}

		b, _ := json.Marshal(out)
		prefix := "auto_repair"
		if in.DryRun {
			prefix = "auto_repair (dry-run)"
		}
		return fmt.Sprintf("%s: %d fixed, %d skipped, %d failed\n%s",
			prefix, len(out.Fixed), len(out.Skipped), len(out.Failed), string(b)), nil
	},
}

//nolint:goconst // action names match MCP verbs, not worth extracting
func applyFix(ctx context.Context, svc *Service, fix *SuggestedFix) error {
	switch fix.Action {
	case "set":
		return applySetFix(ctx, svc, fix)
	case "delete":
		return applyDeleteFix(ctx, svc, fix)
	default:
		return fmt.Errorf("unsupported fix action: %s", fix.Action) //nolint:err113 // internal
	}
}

func applySetFix(ctx context.Context, svc *Service, fix *SuggestedFix) error {
	id, _ := fix.Params["id"].(string)
	field, _ := fix.Params["field"].(string)
	value, _ := fix.Params["value"].(string)
	force, _ := fix.Params["force"].(bool)
	if id == "" || field == "" {
		return fmt.Errorf("set fix missing id or field") //nolint:err113 // internal
	}
	results, err := svc.Proto.SetField(ctx, []string{id}, field, value, parchment.SetFieldOptions{Force: force})
	if err != nil {
		return err
	}
	if len(results) > 0 && !results[0].OK {
		return fmt.Errorf("set failed: %s", results[0].Error) //nolint:err113 // internal
	}
	return nil
}

func applyDeleteFix(ctx context.Context, svc *Service, fix *SuggestedFix) error {
	id, _ := fix.Params["id"].(string)
	if id == "" {
		return fmt.Errorf("delete fix missing id") //nolint:err113 // internal
	}
	return svc.Proto.DeleteArtifact(ctx, id, false)
}
