package cmds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	errFileRequired    = errors.New("--file (-f) is required")
	errUnsupportedKind = errors.New("unsupported resource kind")
)

func ApplyCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply CRD resource definitions to the schema store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return errFileRequired
			}
			svc, cleanup := MustService()
			defer cleanup()
			ctx := context.Background()

			resources, err := loadResources(file)
			if err != nil {
				return err
			}
			for _, r := range resources {
				store := svc.Proto.Store()
				result, err := applyCRDResource(ctx, svc.Proto, store, r)
				if err != nil {
					return fmt.Errorf("apply %s %s: %w", r.Kind, r.Metadata.Name, err)
				}
				fmt.Println(result)
			}
			svc.Proto.Registry().ReloadTraits(ctx)
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "file, directory, or - for stdin")
	return cmd
}

type crdResource struct {
	APIVersion string  `yaml:"apiVersion"`
	Kind       string  `yaml:"kind"`
	Metadata   crdMeta `yaml:"metadata"`
	Spec       crdSpec `yaml:"spec"`
}

type crdMeta struct {
	Name        string `yaml:"name"`
	ID          string `yaml:"id"`
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
}

type crdArtifactSection struct {
	Name string `yaml:"name"`
	Text string `yaml:"text"`
}

type crdSpec struct {
	Lifecycle        *crdLifecycle `yaml:"lifecycle,omitempty"`
	Sections         *crdSections  `yaml:"sections,omitempty"`
	AllowedChildren  []string      `yaml:"allowedChildren,omitempty"`
	IsContainerKind  bool          `yaml:"isContainerKind,omitempty"`

	WhenToUse        string        `yaml:"whenToUse,omitempty"`
	AgentNote        string        `yaml:"agentNote,omitempty"`
	Implies          string        `yaml:"implies,omitempty"`
	World            string        `yaml:"world,omitempty"`
	EvictionPolicy   string        `yaml:"evictionPolicy,omitempty"`
	HalfLifeDays     float64       `yaml:"halfLifeDays,omitempty"`
	AlwaysApply      bool          `yaml:"alwaysApply,omitempty"`
	RequiredSections []string      `yaml:"requiredSections,omitempty"`
	Terminal         bool          `yaml:"terminal,omitempty"`
	Readonly         bool          `yaml:"readonly,omitempty"`
	Directionality   string        `yaml:"directionality,omitempty"`
	AllowedPairs     []crdKindPair `yaml:"allowedPairs,omitempty"`
	Semantics        string        `yaml:"semantics,omitempty"`

	// Artifact CRD fields. Use "content" for sections to avoid conflict with
	// the LabelDefinition "sections" field (which is a struct, not a list).
	Labels          []string             `yaml:"labels,omitempty"`
	ArtifactContent []crdArtifactSection `yaml:"content,omitempty"`
	Goal            string               `yaml:"goal,omitempty"`
	ArtifactParent  string               `yaml:"parent,omitempty"`
	DependsOn       []string             `yaml:"dependsOn,omitempty"`
	Links           map[string][]string  `yaml:"links,omitempty"`
	Extra           map[string]any       `yaml:"extra,omitempty"`
}

type crdLifecycle struct {
	DefaultStatus string          `yaml:"defaultStatus,omitempty"`
	Terminal      bool            `yaml:"terminal,omitempty"`
	Readonly      bool            `yaml:"readonly,omitempty"`
	Transitions   []crdTransition `yaml:"transitions,omitempty"`
}

type crdTransition struct {
	From string   `yaml:"from"`
	To   []string `yaml:"to"`
}

type crdSections struct {
	Must   []string `yaml:"must,omitempty"`
	Should []string `yaml:"should,omitempty"`
	Could  []string `yaml:"could,omitempty"`
}

type crdKindPair struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

func parseCRDFile(data []byte) ([]*crdResource, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var resources []*crdResource
	for {
		var r crdResource
		err := dec.Decode(&r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if r.APIVersion == "" && r.Kind == "" {
			continue
		}
		resources = append(resources, &r)
	}
	return resources, nil
}

func loadResources(target string) ([]*crdResource, error) {
	if target == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		return parseCRDFile(data)
	}

	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		entries, err := os.ReadDir(target)
		if err != nil {
			return nil, err
		}
		var all []*crdResource
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(target, e.Name())) //nolint:gosec // operator-supplied path
			if err != nil {
				return nil, err
			}
			rs, err := parseCRDFile(data)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", e.Name(), err)
			}
			all = append(all, rs...)
		}
		return all, nil
	}

	data, err := os.ReadFile(target) //nolint:gosec // operator-supplied path
	if err != nil {
		return nil, err
	}
	return parseCRDFile(data)
}

func crdResourceID(name string) string {
	sanitized := strings.NewReplacer(".", "-", ":", "-").Replace(name)
	return "RDEF-" + sanitized
}

func applyCRDResource(ctx context.Context, proto *parchment.Protocol, s parchment.Store, r *crdResource) (string, error) {
	switch r.Kind {
	case "LabelDefinition":
		if err := applyLabelDefinitionCRD(ctx, s, r); err != nil {
			return "", err
		}
		return fmt.Sprintf("applied LabelDefinition %s", r.Metadata.Name), nil
	case "Artifact":
		return applyArtifactCRD(ctx, proto, r)
	default:
		return "", fmt.Errorf("%w: %s", errUnsupportedKind, r.Kind)
	}
}

func applyArtifactCRD(ctx context.Context, proto *parchment.Protocol, r *crdResource) (string, error) {
	id := r.Metadata.ID
	if id == "" {
		id = r.Metadata.Name
	}
	if id == "" {
		return "", fmt.Errorf("artifact CRD requires metadata.id") //nolint:err113 // user-facing validation
	}
	title := r.Metadata.Title
	if title == "" {
		title = r.Metadata.Name
	}

	sections := make([]parchment.ArtifactSection, 0, len(r.Spec.ArtifactContent))
	for _, sec := range r.Spec.ArtifactContent {
		sections = append(sections, parchment.ArtifactSection{Name: sec.Name, Text: strings.TrimSpace(sec.Text)})
	}

	pr := &parchment.Resource{
		APIVersion: r.APIVersion,
		Kind:       r.Kind,
		Metadata: parchment.ResourceMeta{
			Name:  r.Metadata.Name,
			ID:    id,
			Title: title,
		},
		Spec: parchment.ResourceSpec{
			Labels:          r.Spec.Labels,
			ArtifactContent: sections,
			Goal:            r.Spec.Goal,
			ArtifactParent:  r.Spec.ArtifactParent,
			DependsOn:       r.Spec.DependsOn,
			Links:           r.Spec.Links,
			Extra:           r.Spec.Extra,
		},
	}

	result, err := parchment.ApplyArtifactResource(ctx, proto, pr)
	if err != nil {
		return "", err
	}
	verb := "updated"
	if result.Created {
		verb = "created"
	}
	return fmt.Sprintf("applied Artifact %s (%s)", result.Artifact.ID, verb), nil
}

func applyLabelDefinitionCRD(ctx context.Context, s parchment.Store, r *crdResource) error {
	trait := map[string]any{
		"world":             r.Spec.World,
		"eviction_policy":   r.Spec.EvictionPolicy,
		"half_life_days":    int(r.Spec.HalfLifeDays),
		"always_apply":      r.Spec.AlwaysApply,
		"is_container_kind": r.Spec.IsContainerKind,
	}
	if r.Spec.Lifecycle != nil {
		trait["default_status"] = r.Spec.Lifecycle.DefaultStatus
		trait["terminal"] = r.Spec.Lifecycle.Terminal
		trait["readonly"] = r.Spec.Lifecycle.Readonly
	}
	if r.Spec.Sections != nil {
		trait["must_sections"] = r.Spec.Sections.Must
		trait["should_sections"] = r.Spec.Sections.Should
		trait["could_sections"] = r.Spec.Sections.Could
	}
	if r.Spec.AllowedChildren != nil {
		trait["allowed_children"] = r.Spec.AllowedChildren
	}
	if r.Spec.RequiredSections != nil {
		trait["required_sections"] = r.Spec.RequiredSections
	}
	cleanExtra(trait)

	now := time.Now().UTC()
	id := crdResourceID(r.Metadata.Name)
	art := &parchment.Artifact{
		ID:         id,
		Labels:     []string{parchment.LabelPrefixKind + "label_definition", "work.active", parchment.LabelPrefixScope + parchment.SchemaScope},
		Title:      r.Metadata.Name,
		Extra:      trait,
		CreatedAt:  now,
		UpdatedAt:  now,
		InsertedAt: now,
	}
	if r.Spec.WhenToUse != "" {
		art.Sections = append(art.Sections, parchment.Section{Name: "when_to_use", Text: strings.TrimSpace(r.Spec.WhenToUse)})
	}
	if r.Spec.AgentNote != "" {
		art.Sections = append(art.Sections, parchment.Section{Name: "agent_note", Text: strings.TrimSpace(r.Spec.AgentNote)})
	}
	if r.Spec.Implies != "" {
		art.Sections = append(art.Sections, parchment.Section{Name: "implies", Text: strings.TrimSpace(r.Spec.Implies)})
	}
	return s.Put(ctx, art)
}



func cleanExtra(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if val == "" {
				delete(m, k)
			}
		case bool:
			if !val {
				delete(m, k)
			}
		case int:
			if val == 0 {
				delete(m, k)
			}
		case []string:
			if len(val) == 0 {
				delete(m, k)
			}
		}
	}
}
