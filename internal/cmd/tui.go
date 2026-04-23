package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/screen"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive TUI browser",
		Long: `Launch an interactive terminal UI for browsing JumpCloud resources.

Navigate resources with keyboard shortcuts:
  j/k or arrows  Move cursor
  Enter           Open resource / drill into detail
  /               Filter resources
  s               Cycle sort field
  a               Toggle all fields
  r               Refresh data
  Esc             Go back
  q               Quit`,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries := tui.BuildRegistry()
			home := screen.NewHomeScreen(entries)
			app := tui.NewApp(home)
			app.NewHelpScreen = func() tui.Screen { return screen.NewHelpScreen() }

			// Wire the recipe dispatcher so the Recipes screen can execute steps.
			// Use newRootCmdForRecipeStep which resets persistent-Set viper keys
			// between steps so -t in step N doesn't corrupt step N+1's output
			// format (see comments on resetViperForRecipeStep in recipe.go).
			screen.RecipeDispatcher = recipe.NewDispatcher(newRootCmdForRecipeStep)

			p := tea.NewProgram(app, tea.WithAltScreen())
			// Register the program so the recipe-run goroutine can post
			// completion messages back onto the update loop.
			screen.RegisterTeaProgram(p)

			_, err := p.Run()
			return err
		},
	}
}
