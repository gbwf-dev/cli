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
	"github.com/go-git/go-git/v5/config"
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

const (
	VanillaFlag       = "vanilla"
	VanillaRemoteName = "gbwf"
	VanillaRemoteURL  = "https://github.com/gbwf-dev/vanilla.git"

	DepthFlag = "depth"
	Depth     = 1
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringP(VanillaFlag, string(VanillaFlag[0]), VanillaRemoteURL, "sets the vanilla remote to pull from")
	initCmd.Flags().IntP(DepthFlag, string(DepthFlag[0]), Depth, "limit fetch depth to N commits (shallow clone/pull)")
}

func RunE(cmd *cobra.Command, args []string) error {
	stdin := cmd.InOrStdin()
	stdout := cmd.OutOrStdout()

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
			tea.WithOutput(stdout),
			tea.WithInput(stdin),
		)
		if _, err := program.Run(); err != nil {
			return fmt.Errorf("prompt failed: %w", err)
		}

		if !prompt.GetResult() {
			fmt.Fprintln(stdout, "Repository initialization cancelled")
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

	fmt.Fprintln(stdout, "Git repository ready at:", targetDir, repo)

	flags := cmd.Flags()

	var vanillaURL string
	vanillaURL, err = flags.GetString(VanillaFlag)
	if err != nil {
		return err
	}

	var vanilla *git.Remote
	remoteConfig := &config.RemoteConfig{Name: VanillaRemoteName, URLs: []string{vanillaURL}}
	vanilla, err = repo.CreateRemote(remoteConfig)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Pulling from %s %s...\n", vanilla.Config().Name, vanillaURL)
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}

	var depth int
	depth, err = cmd.Flags().GetInt(DepthFlag)
	if err != nil {
		return err
	}

	return wt.Pull(&git.PullOptions{
		RemoteName:   remoteConfig.Name,
		SingleBranch: true,
		Depth:        depth,
		Progress:     stdout,
	})
}
