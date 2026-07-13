package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/kevynsax/publish/internal/publish"
	"github.com/spf13/cobra"
)

var (
	cfg = publish.NewConfig()
	ui  = publish.NewUI()
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "publish [mode]",
	Short: "Cross Exchange release tool — bump, commit, push, and update k8s manifests",
	Long: `publish walks every microservice repo, asks a local LLM to write a commit
message and choose the semver bump, then commits + pushes each repo and
updates the Kubernetes deploy manifests.`,
	// Running 'publish' with no subcommand defaults to dev.
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDev()
	},
	SilenceUsage: true,
}

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Bump + commit + push each changed repo, then update k8s dev-cross/ (default)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDev()
	},
}

var prodCmd = &cobra.Command{
	Use:   "prod",
	Short: "Promote current repo versions into k8s cross/ (prod) and push",
	RunE: func(cmd *cobra.Command, args []string) error {
		return publish.NewRunner(cfg, ui).RunProd()
	},
}

var allCmd = &cobra.Command{
	Use:   "all",
	Short: "Check, deploy to dev, then (if dev succeeds) promote to prod",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureEngine(); err != nil {
			return err
		}
		return publish.NewRunner(cfg, ui).RunAll()
	},
}

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Wipe the decision cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := os.RemoveAll(cfg.CacheDir); err != nil {
			return err
		}
		fmt.Printf("cleared decision cache: %s\n", cfg.CacheDir)
		return nil
	},
}

func runDev() error {
	if err := ensureEngine(); err != nil {
		return err
	}
	return publish.NewRunner(cfg, ui).RunDev()
}

func ensureEngine() error {
	switch cfg.Engine {
	case publish.EngineClaude:
		if _, err := findCLI("claude"); err != nil {
			return fmt.Errorf("'claude' CLI not found — needed for --smart")
		}
	case publish.EngineCodex:
		if _, err := findCLI("codex"); err != nil {
			return fmt.Errorf("'codex' CLI not found — needed for --codex")
		}
	default:
		return publish.EnsureOllama(cfg)
	}
	return nil
}

// normFolder strips ./ prefix, trailing slash, and takes the basename so
// "./shared/", "shared/", and "/path/to/shared" all resolve to "shared".
func normFolder(f string) string {
	f = strings.TrimPrefix(f, "./")
	f = strings.TrimSuffix(f, "/")
	if idx := strings.LastIndexByte(f, '/'); idx >= 0 {
		f = f[idx+1:]
	}
	return f
}

func findCLI(name string) (string, error) {
	// check PATH via exec.LookPath equivalent
	paths := strings.Split(os.Getenv("PATH"), ":")
	for _, p := range paths {
		if p == "" {
			continue
		}
		full := p + "/" + name
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH", name)
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(
		&lazyFlag, "lazy", "l", false,
		fmt.Sprintf("Thorough local model (%s) — slower but more nuanced", cfg.LazyModel),
	)
	rootCmd.PersistentFlags().BoolVarP(
		&smartFlag, "slow", "s", false,
		"Smart but slow: Claude Code headless (Haiku) on your subscription",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&codexFlag, "codex", "c", false,
		"Smart and fast: codex exec (gpt-5.5, structured JSON)",
	)
	rootCmd.PersistentFlags().StringVarP(
		&cfg.FolderFilter, "folder", "f", "",
		"Restrict to a single repo folder; k8s manifests are still updated for that repo",
	)
	rootCmd.PersistentFlags().BoolVarP(
		&cfg.DryRun, "dry-run", "d", false,
		"Preview only — no edits, commits or pushes",
	)

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		applyEngineFlags()
		cfg.FolderFilter = normFolder(cfg.FolderFilter)
		return nil
	}

	folderCompleter := func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		pathPrefix := ""
		if strings.HasPrefix(toComplete, "./") {
			pathPrefix = "./"
		}
		prefix := normFolder(toComplete)
		var completions []string
		for _, r := range publish.AllKnownRepos {
			if strings.HasPrefix(r, prefix) {
				completions = append(completions, pathPrefix+r)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
	for _, cmd := range []*cobra.Command{rootCmd, devCmd, prodCmd, allCmd} {
		cmd.RegisterFlagCompletionFunc("folder", folderCompleter) //nolint:errcheck
	}

	rootCmd.AddCommand(devCmd, prodCmd, allCmd, cleanCmd)
}

var (
	lazyFlag  bool
	smartFlag bool
	codexFlag bool
)

func applyEngineFlags() {
	switch {
	case smartFlag:
		cfg.Engine = publish.EngineClaude
	case codexFlag:
		cfg.Engine = publish.EngineCodex
	case lazyFlag:
		cfg.Engine = publish.EngineOllama
		cfg.OllamaModel = cfg.LazyModel
	}
}
