package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/dpopsuev/scribe/config"
	"github.com/spf13/cobra"
)

const lintLevelError = "error"

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
