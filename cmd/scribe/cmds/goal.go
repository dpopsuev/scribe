package cmds

import (
	"context"
	"fmt"

	"github.com/dpopsuev/scribe/service"
	"github.com/spf13/cobra"
)

// GoalCmd returns the goal command group.
func GoalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "goal",
		Short: "Manage the current goal (short-term north star)",
	}
	var in service.SetGoalInput
	setGoalCmd := &cobra.Command{
		Use:   "set <title>",
		Short: "Set the current goal (retires any previous, creates a root delivery artifact)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, close := MustService()
			defer close()
			in.Title = args[0]
			res, err := svc.SetGoal(context.Background(), in)
			if err != nil {
				return err
			}
			for _, a := range res.Archived {
				fmt.Printf("archived %s: %s\n", a.ID, a.Title)
			}
			fmt.Printf("%s [current] %s\n", res.Goal.ID, res.Goal.Title)
			fmt.Printf("%s [draft] %s (justifies %s)\n", res.Root.ID, res.Root.Title, res.Goal.ID)
			return nil
		},
	}
	setGoalCmd.Flags().StringVar(&in.Scope, "scope", "", "scope for the goal")
	setGoalCmd.Flags().StringVar(&in.Kind, "kind", "goal", "kind for the root delivery artifact")

	showGoalCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the current goal",
		RunE: func(_ *cobra.Command, _ []string) error {
			svc, close := MustService()
			defer close()
			m, _ := svc.Motd(context.Background())
			if len(m.Goals) == 0 {
				fmt.Println("no current goal set")
				return nil
			}
			for _, a := range m.Goals {
				prefix := ""
				if a.Scope != "" {
					prefix = "[" + a.Scope + "] "
				}
				fmt.Printf("%s %s%s\n", a.ID, prefix, a.Title)
			}
			return nil
		},
	}

	cmd.AddCommand(setGoalCmd, showGoalCmd)
	return cmd
}
