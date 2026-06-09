package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/service"
	"github.com/spf13/cobra"
)

const lintLevelError = "error"

func BriefCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "brief",
		Short: "Session brief: active goal, open work, and recent knowledge",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := MustConfig()
			cwd, _ := os.Getwd()
			var homeScopes []string
			if sc := cfg.ScopeForDir(cwd); sc != "" {
				homeScopes = []string{sc}
			} else {
				homeScopes = cfg.ResolvedScopes()
			}
			svc, closeDB, err := service.Open(cfg, homeScopes)
			if err != nil {
				return err
			}
			defer closeDB()
			m, err := svc.Brief(context.Background())
			if err != nil {
				return err
			}
			var sections []string
			if len(m.Goals) > 0 {
				var lines []string
				for _, g := range m.Goals {
					prefix := ""
					if g.Label(parchment.LabelPrefixScope) != "" {
						prefix = "[" + g.Label(parchment.LabelPrefixScope) + "] "
					}
					lines = append(lines, fmt.Sprintf("  %s %s%s", g.ID, prefix, g.Title))
				}
				sections = append(sections, "Goal:\n"+strings.Join(lines, "\n"))
			}
			if len(m.Warnings) > 0 {
				var lines []string
				for _, w := range m.Warnings {
					lines = append(lines, "  ⚠ "+w)
				}
				sections = append(sections, "Warnings:\n"+strings.Join(lines, "\n"))
			}
			if len(sections) == 0 {
				fmt.Println("nothing to report")
				return nil
			}
			fmt.Println(strings.Join(sections, "\n\n"))
			return nil
		},
	}
}

func DfCmd() *cobra.Command {
	var staleDays int
	var format string
	cmd := &cobra.Command{
		Use:   "df",
		Short: "Housekeeping dashboard: storage, staleness, scope health",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			report, err := svc.Dashboard(context.Background(), staleDays)
			if err != nil {
				return err
			}
			if format == formatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			fmt.Printf("DB size: %d bytes\n\n", report.DBSizeBytes)
			fmt.Println("Scopes:")
			for _, ds := range report.Scopes {
				fmt.Printf("  %-15s total=%d active=%d archived=%d sections=%d edges=%d stale=%d\n",
					ds.Scope, ds.Total, ds.Active, ds.Archived, ds.Sections, ds.Edges, ds.Stale)
			}
			if len(report.StaleArts) > 0 {
				fmt.Println("\nTop stale artifacts (by updated_at):")
				for _, a := range report.StaleArts {
					fmt.Printf("  %s [%s] %s\n", a.ID, a.Label(parchment.LabelPrefixStatus), a.Title)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&staleDays, "stale-days", 30, "staleness threshold in days")
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func VacuumCmd() *cobra.Command {
	var days int
	var scope string
	var force bool
	cmd := &cobra.Command{
		Use:   "vacuum",
		Short: "Delete archived artifacts older than --days (default 90). Protected kinds (spec, bug) are skipped unless --force.",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			result, err := svc.Proto.Vacuum(context.Background(), days, scope, force)
			if err != nil {
				return err
			}
			for _, id := range result.Skipped {
				fmt.Printf("skipped %s (has incoming edges — use --force to override)\n", id)
			}
			if len(result.Deleted) == 0 {
				fmt.Println("nothing to vacuum")
				return nil
			}
			for _, id := range result.Deleted {
				fmt.Printf("deleted %s\n", id)
			}
			fmt.Printf("%d archived artifacts vacuumed\n", len(result.Deleted))
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 90, "minimum age in days")
	cmd.Flags().StringVar(&scope, "scope", "", "limit to artifacts in this scope")
	cmd.Flags().BoolVar(&force, "force", false, "delete protected kinds (spec, bug)")
	return cmd
}

func InventoryCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Show a dashboard summary of all artifacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			inv, err := svc.Inventory(context.Background())
			if err != nil {
				return err
			}
			if format == formatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(inv)
			}
			fmt.Printf("Total artifacts: %d\n\n", inv.Total)
			fmt.Println("By kind:")
			for k, v := range inv.ByKind {
				fmt.Printf("  %-15s %d\n", k, v)
			}
			fmt.Println("\nBy status:")
			for s, v := range inv.ByStatus {
				fmt.Printf("  %-15s %d\n", s, v)
			}
			for kind, arts := range inv.Tracked {
				if len(arts) == 0 {
					continue
				}
				fmt.Printf("\nTracked %s:\n", kind)
				for _, a := range arts {
					prefix := ""
					if a.Label(parchment.LabelPrefixScope) != "" {
						prefix = "[" + a.Label(parchment.LabelPrefixScope) + "] "
					}
					fmt.Printf("  %s %s%s\n", a.ID, prefix, a.Title)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func LintCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate the resolved schema for internal consistency",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			results := svc.Proto.Lint()
			if format == formatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}
			errors, warnings := 0, 0
			for _, r := range results {
				switch r.Level {
				case lintLevelError:
					errors++
					fmt.Printf("ERROR %s\n", r.Message)
				case "warn":
					warnings++
					fmt.Printf("WARN  %s\n", r.Message)
				}
			}
			if errors == 0 && warnings == 0 {
				fmt.Println("OK    schema is valid")
			} else {
				fmt.Printf("OK    schema validated (%d error(s), %d warning(s))\n", errors, warnings)
			}
			if errors > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func CheckCmd() *cobra.Command {
	var scope, format string
	var fix bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate DB artifacts against the resolved schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			ctx := context.Background()

			if fix {
				report, fixes, err := svc.Proto.CheckFix(ctx, scope)
				if err != nil {
					return err
				}
				if format == formatJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(map[string]any{
						"report": report,
						"fixes":  fixes,
					})
				}
				for _, v := range report.Violations {
					fmt.Printf("%-18s %-18s %s\n", v.ID, v.Category, v.Detail)
				}
				if len(fixes) > 0 {
					fmt.Println("\nFixes applied:")
					for _, f := range fixes {
						fmt.Printf("  %s\n", f)
					}
				}
				fmt.Printf("\nScanned %d, passed %d, violations %d, fixes %d\n",
					report.TotalScanned, report.TotalPassed, report.TotalViolations, len(fixes))
				if report.TotalViolations > 0 {
					os.Exit(1)
				}
				return nil
			}

			report, err := svc.Proto.Check(ctx, scope)
			if err != nil {
				return err
			}
			if format == formatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			for _, v := range report.Violations {
				fmt.Printf("%-18s %-18s %s\n", v.ID, v.Category, v.Detail)
			}
			fmt.Printf("\nScanned %d, passed %d, violations %d\n",
				report.TotalScanned, report.TotalPassed, report.TotalViolations)
			if report.TotalViolations > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "limit to a specific scope")
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	cmd.Flags().BoolVar(&fix, "fix", false, "auto-repair fixable violations")
	return cmd
}

func ConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Dump resolved configuration as YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Resolve(ConfigPath)
			if err != nil {
				return err
			}
			if DBPath != "" {
				cfg.DB.SQLite.Path = DBPath
			}
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
}
