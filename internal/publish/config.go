package publish

import (
	"os"
	"path/filepath"
	"strings"
)

// Config holds every tunable value for a publish run.
type Config struct {
	ProjectsDir    string
	K8SDir         string
	OllamaURL      string
	OllamaModel    string
	LazyModel      string
	Engine         EngineType
	ClaudeModel    string
	CodexModel     string
	CodexReasoning string
	CacheDir       string
	PromptsDir     string
	FolderFilter   string
	DryRun         bool
	NoCache        bool
	DecisionPrompt string
	ModelID        string
}

func NewConfig() *Config {
	projectsDir := resolveProjectsDir()
	return &Config{
		ProjectsDir:    projectsDir,
		K8SDir:         getenv("K8S_DIR", "/Users/kevynklava/WebstormProjects/k8s"),
		OllamaURL:      getenv("OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:    getenv("OLLAMA_MODEL", "qwen2.5:3b"),
		LazyModel:      getenv("PUBLISH_LAZY_MODEL", "gemma4"),
		Engine:         EngineOllama,
		ClaudeModel:    getenv("CLAUDE_MODEL", "haiku"),
		CodexModel:     getenv("CODEX_MODEL", "gpt-5.5"),
		CodexReasoning: getenv("CODEX_REASONING", "low"),
		CacheDir:       getenv("PUBLISH_CACHE_DIR", defaultCacheDir(projectsDir)),
		PromptsDir:     getenv("PROMPTS_DIR", projectsDir+"/prompts"),
		NoCache:        os.Getenv("PUBLISH_NO_CACHE") == "1",
	}
}

// resolveProjectsDir picks the folder to operate on. An explicit PROJECTS_DIR
// always wins. Otherwise the current working directory is used when it is itself
// a git repo (single-repo project, e.g. book-reader) or contains git repos
// (multi-repo project, e.g. cross). Failing that, it falls back to the cross root.
func resolveProjectsDir() string {
	if v := os.Getenv("PROJECTS_DIR"); v != "" {
		return v
	}
	if cwd, err := os.Getwd(); err == nil {
		if isGitRepo(cwd) || hasGitChildren(cwd) {
			return cwd
		}
	}
	return "/Users/kevynklava/projects/cross"
}

// defaultCacheDir keeps the decision cache next to a multi-repo container (as
// publish.sh did) but, for a single-repo project, outside the repo so a blanket
// `git add -A` never commits it.
func defaultCacheDir(projectsDir string) string {
	if isGitRepo(projectsDir) {
		return filepath.Join(os.TempDir(), "publish-cache", filepath.Base(projectsDir))
	}
	return projectsDir + "/.publish-cache"
}

func hasGitChildren(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && isGitRepo(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// AllRepos lists every deployable repo in publish order.
var AllRepos = []string{
	"authentication-web-app", "binance-web-app", "client-web-app", "landing-page",
	"notification-api", "settings-api", "user-api", "exchange-api",
	"binance-adapter-api", "websocket-gateway-api", "yahoo-adapter-api",
}

// AllKnownRepos includes tag-only repos (e.g. shared) for completion purposes.
var AllKnownRepos = append(AllRepos, "shared")

var tagRepos = buildTagRepos()

func buildTagRepos() map[string]bool {
	m := map[string]bool{"shared": true}
	if env := os.Getenv("TAG_REPOS"); env != "" {
		m = map[string]bool{}
		for _, r := range strings.Fields(env) {
			m[r] = true
		}
	}
	return m
}

func IsTagRepo(name string) bool { return tagRepos[name] }

func ImageName(repo string) string {
	names := map[string]string{
		"authentication-web-app": "auth-web-app",
		"binance-web-app":        "binance-web-app",
		"client-web-app":         "client-web-app",
		"landing-page":           "landing-page",
		"notification-api":       "notification-api",
		"settings-api":           "settings-api",
		"user-api":               "user-api",
		"exchange-api":           "exchange-api",
		"binance-adapter-api":    "binance-adapter-api",
		"websocket-gateway-api":  "websocket-gateway-api",
		"yahoo-adapter-api":      "yahoo-adapter-api",
		"bank-conciliation":      "cross-helpers/bank-conciliation",
		"interview-challenger":   "cross-helpers/interview-challenger",
	}
	return names[repo]
}

func (c *Config) ResolveModelID() string {
	switch c.Engine {
	case EngineClaude:
		return "claude:" + c.ClaudeModel
	case EngineCodex:
		return "codex:" + c.CodexModel + "/" + c.CodexReasoning
	default:
		return c.OllamaModel
	}
}

// LoadPrompt resolves and caches the decision prompt + model ID.
func (c *Config) LoadPrompt() string {
	c.ModelID = c.ResolveModelID()

	if pf := os.Getenv("PUBLISH_PROMPT_FILE"); pf != "" {
		if data, err := os.ReadFile(pf); err == nil {
			c.DecisionPrompt = string(data)
			return pf
		}
	}

	var promptFile string
	if c.Engine == EngineClaude {
		promptFile = c.PromptsDir + "/decision.claude.txt"
	} else if strings.HasPrefix(c.OllamaModel, "qwen") {
		promptFile = c.PromptsDir + "/decision.qwen1.txt"
	}
	if promptFile != "" {
		if data, err := os.ReadFile(promptFile); err == nil {
			c.DecisionPrompt = string(data)
			return promptFile
		}
	}

	c.DecisionPrompt = LLMSystem
	return "built-in default"
}
