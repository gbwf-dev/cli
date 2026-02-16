package cmd

import (
	"fmt"
	"os"

	"gbwf/components"
	"gbwf/manifest"
	"gbwf/source"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a GBWF App",
	Long:  `Starts the cli process`,

	RunE: RunE,

	SilenceUsage: true,
}

const (
	ManifestFlag = "manifest"
	Manifest     = "https://raw.githubusercontent.com/gbwf-dev/cli/refs/heads/feature/manifest/manifest.yaml"

	DepthFlag = "depth"
	Depth     = 1
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().StringP(ManifestFlag, string(ManifestFlag[0]), Manifest, "sets the manifest")
	initCmd.Flags().IntP(DepthFlag, string(DepthFlag[0]), Depth, "limit fetch depth to N commits (shallow clone/pull)")
}

func RunE(cmd *cobra.Command, args []string) error {
	flags := cmd.Flags()

	manifestFlag, err := flags.GetString(ManifestFlag)
	if err != nil {
		return err
	}

	reader, err := source.Resolve(manifestFlag)
	if err != nil {
		return err
	}
	defer reader.Close()

	decodedManifest := new(manifest.Manifest)

	err = yaml.NewDecoder(reader).Decode(decodedManifest)
	if err != nil {
		return err
	}

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

	var repo *git.Repository
	repo, err = git.PlainInit(targetDir, false)
	if err != nil {
		return err
	}

	stdin := cmd.InOrStdin()
	stdout := cmd.OutOrStdout()

	var bases []manifest.Base
	for _, base := range decodedManifest.Base {
		bases = append(bases, base)
	}
	selector := components.NewBaseSelector(bases...)
	program := tea.NewProgram(
		selector,
		tea.WithInput(stdin),
		tea.WithOutput(stdout),
		tea.WithContext(cmd.Context()),
	)
	if _, err = program.Run(); err != nil {
		return err
	}

	base := selector.Selected()
	if base == nil {
		return nil
	}

	repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
	})

	return err
}
