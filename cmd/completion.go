package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var installCompletion bool

var completionCmd = &cobra.Command{
	Use:   "completion <shell>",
	Short: "Generate or install shell completion",
	Long: `Generate or install a shell completion script for publish.

Supported shells: zsh, bash, fish, powershell

Quick install:
  publish completion zsh --install
  publish completion bash --install

Manual install:
  publish completion zsh  > ~/.zsh/completions/_publish
  publish completion bash > /usr/local/etc/bash_completion.d/publish

After installing, open a new terminal tab to activate.`,
}

var completionZshCmd = &cobra.Command{
	Use:   "zsh",
	Short: "Generate zsh completion script",
	Long: `Generate the zsh completion script for publish.

Install automatically (recommended):
  publish completion zsh --install

Install manually:
  publish completion zsh > ~/.zsh/completions/_publish
  # then open a new terminal tab or run: exec zsh

To update after a publish binary upgrade, just re-run --install.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if installCompletion {
			return installZsh()
		}
		return rootCmd.GenZshCompletion(os.Stdout)
	},
}

var completionBashCmd = &cobra.Command{
	Use:   "bash",
	Short: "Generate bash completion script",
	Long: `Generate the bash completion script for publish.

Install automatically:
  publish completion bash --install

Install manually (macOS with Homebrew bash-completion):
  publish completion bash > /usr/local/etc/bash_completion.d/publish

Install manually (Linux):
  publish completion bash > /etc/bash_completion.d/publish`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if installCompletion {
			return installBash()
		}
		return rootCmd.GenBashCompletion(os.Stdout)
	},
}

var completionFishCmd = &cobra.Command{
	Use:   "fish",
	Short: "Generate fish completion script",
	Long: `Generate the fish completion script for publish.

Install automatically:
  publish completion fish --install

Install manually:
  publish completion fish > ~/.config/fish/completions/publish.fish`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if installCompletion {
			return installFish()
		}
		return rootCmd.GenFishCompletion(os.Stdout, true)
	},
}

func installZsh() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".zsh", "completions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create completions dir: %w", err)
	}
	path := filepath.Join(dir, "_publish")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create completion file: %w", err)
	}
	defer f.Close()
	if err := rootCmd.GenZshCompletion(f); err != nil {
		return err
	}
	// clear zsh completion cache so new completions load immediately
	matches, _ := filepath.Glob(filepath.Join(home, ".zcompdump*"))
	for _, m := range matches {
		os.Remove(m) //nolint:errcheck
	}
	fmt.Printf("installed zsh completion → %s\n", path)
	fmt.Println("open a new terminal tab (or run: exec zsh) to activate")
	return nil
}

func installBash() error {
	// try Homebrew path first, fall back to /etc
	candidates := []string{
		"/usr/local/etc/bash_completion.d",
		"/opt/homebrew/etc/bash_completion.d",
		"/etc/bash_completion.d",
	}
	dir := ""
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			dir = c
			break
		}
	}
	if dir == "" {
		return fmt.Errorf("no bash_completion.d directory found; install manually:\n  publish completion bash > /usr/local/etc/bash_completion.d/publish")
	}
	path := filepath.Join(dir, "publish")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create completion file: %w", err)
	}
	defer f.Close()
	if err := rootCmd.GenBashCompletion(f); err != nil {
		return err
	}
	fmt.Printf("installed bash completion → %s\n", path)
	fmt.Println("open a new terminal tab to activate")
	return nil
}

func installFish() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".config", "fish", "completions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create completions dir: %w", err)
	}
	path := filepath.Join(dir, "publish.fish")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create completion file: %w", err)
	}
	defer f.Close()
	if err := rootCmd.GenFishCompletion(f, true); err != nil {
		return err
	}
	fmt.Printf("installed fish completion → %s\n", path)
	return nil
}

func init() {
	// Disable cobra's auto-generated completion command; we provide our own.
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	for _, sub := range []*cobra.Command{completionZshCmd, completionBashCmd, completionFishCmd} {
		sub.Flags().BoolVar(&installCompletion, "install", false, "Install the completion script to the standard path")
	}

	completionCmd.AddCommand(completionZshCmd, completionBashCmd, completionFishCmd)
	rootCmd.AddCommand(completionCmd)
}
