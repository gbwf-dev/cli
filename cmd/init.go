/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"cli/components"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a GBWF App",
	Long:  `Starts the cli process`,
	RunE:  RunE,

	SilenceUsage: true,
}

func RunE(cmd *cobra.Command, args []string) error {
	// Get current working directory
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Determine the target directory (use first arg if provided, else current dir)
	targetDir := dir
	if len(args) > 0 && args[0] != "" {
		targetDir = args[0]
	}

	// Try opening existing Git repository
	repo, err := git.PlainOpen(filepath.Join(targetDir, ".git"))
	if errors.Is(err, git.ErrRepositoryNotExists) {
		// Prompt user to initialize a new repository
		prompt := components.NewYesNo("GBWF needs a git repository, do you want to initialize one?")
		program := tea.NewProgram(
			prompt,
			tea.WithOutput(cmd.OutOrStdout()),
			tea.WithInput(cmd.InOrStdin()),
		)
		if _, err := program.Run(); err != nil {
			return fmt.Errorf("prompt failed: %w", err)
		}

		if !prompt.GetResult() {
			fmt.Fprintln(cmd.OutOrStdout(), "Repository initialization cancelled")
			return nil
		}

		// Initialize a new Git repository
		repo, err = git.PlainInit(targetDir, false)
		if err != nil {
			return fmt.Errorf("failed to initialize git repository: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to open git repository: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Git repository ready at:", targetDir, repo)

	return nil
}

func init() {
	rootCmd.AddCommand(initCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// initCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// initCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
