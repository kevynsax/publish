package publish

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Decide queries the configured LLM engine for a repo decision, using the cache when available.
func Decide(cfg *Config, repoName, curVer, ctx string) CachedDecision {
	key := CacheKeyFor(cfg.ModelID, cfg.DecisionPrompt, ctx)

	if !cfg.NoCache {
		if d, ok := ReadCache(cfg.CacheDir, repoName, key); ok {
			return CachedDecision{Source: CacheHit, Decision: d}
		}
	}

	body := fmt.Sprintf("Repository: %s (current version %s)\n\n%s", repoName, curVer, ctx)

	var d *Decision
	var err error
	switch cfg.Engine {
	case EngineClaude:
		d, err = claudeCall(cfg, cfg.DecisionPrompt, body)
	case EngineCodex:
		d, err = codexCall(cfg, cfg.DecisionPrompt, body)
	default:
		d, err = ollamaCall(cfg, cfg.DecisionPrompt, body)
	}

	if err != nil || d == nil {
		fallback := Decision{
			Bump:    BumpPatch,
			Message: "Update " + repoName,
			Reason:  "LLM unavailable, defaulted to patch",
		}
		return CachedDecision{Source: CacheDown, Decision: fallback}
	}

	if d.Message == "" {
		d.Message = "Update " + repoName
	}
	_ = WriteCache(cfg.CacheDir, repoName, key, *d)
	return CachedDecision{Source: CacheMiss, Decision: *d}
}

// EnsureOllama checks that the ollama API is reachable, auto-starting it if needed.
func EnsureOllama(cfg *Config) error {
	client := &http.Client{Timeout: 5 * time.Second}
	if _, err := client.Get(cfg.OllamaURL + "/api/version"); err == nil {
		return nil
	}
	if _, err := exec.LookPath("ollama"); err != nil {
		return fmt.Errorf("ollama not reachable at %s and the 'ollama' CLI is not installed", cfg.OllamaURL)
	}
	u, _ := url.Parse(cfg.OllamaURL)
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "0.0.0.0" {
		return fmt.Errorf("ollama not reachable at %s (remote) — start it there", cfg.OllamaURL)
	}

	fmt.Println("ollama not running — starting it (ollama serve)…")
	logPath := os.TempDir() + "/publish-ollama.log"
	logf, _ := os.Create(logPath)
	cmd := exec.Command("ollama", "serve")
	cmd.Env = append(os.Environ(), "OLLAMA_HOST="+u.Host)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ollama: %w", err)
	}
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		if _, err := client.Get(cfg.OllamaURL + "/api/version"); err == nil {
			fmt.Println("ollama is ready")
			return nil
		}
	}
	return fmt.Errorf("started 'ollama serve' but it didn't come up within 30s (see %s)", logPath)
}

func ollamaCall(cfg *Config, system, prompt string) (*Decision, error) {
	payload := map[string]any{
		"model":   cfg.OllamaModel,
		"stream":  false,
		"format":  json.RawMessage(LLMSchema),
		"options": map[string]any{"temperature": 0},
		"system":  system,
		"prompt":  prompt,
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Post(cfg.OllamaURL+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var result struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Response == "" {
		return nil, fmt.Errorf("empty ollama response")
	}
	return parseDecisionJSON(result.Response)
}

func claudeCall(cfg *Config, system, prompt string) (*Decision, error) {
	cmd := exec.Command("claude", "-p",
		"--model", cfg.ClaudeModel,
		"--system-prompt", system,
		"--max-turns", "1",
		"--output-format", "json",
	)
	cmd.Dir = os.TempDir()
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("claude call failed: %w", err)
	}
	var result struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("claude JSON parse: %w", err)
	}
	return parseDecisionJSON(result.Result)
}

func codexCall(cfg *Config, system, prompt string) (*Decision, error) {
	schemaFile, err := writeTempFile([]byte(CodexSchema))
	if err != nil {
		return nil, err
	}
	defer os.Remove(schemaFile)

	outFile, err := os.CreateTemp("", "publish-codex-out-*.json")
	if err != nil {
		return nil, err
	}
	outFile.Close()
	defer os.Remove(outFile.Name())

	cmd := exec.Command("codex", "exec",
		"--model", cfg.CodexModel,
		"-c", `model_reasoning_effort="`+cfg.CodexReasoning+`"`,
		"--sandbox", "read-only",
		"--skip-git-repo-check",
		"--ephemeral",
		"--color", "never",
		"--output-schema", schemaFile,
		"--output-last-message", outFile.Name(),
		system+"\n\n"+prompt,
	)
	cmd.Dir = os.TempDir()
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		// codex sometimes exits non-zero even on success; check output file
	}

	raw, err := os.ReadFile(outFile.Name())
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("codex produced no output")
	}
	return parseDecisionJSON(string(raw))
}

func parseDecisionJSON(s string) (*Decision, error) {
	s = strings.TrimSpace(s)
	// strip markdown fences if present
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		var inner []string
		for _, l := range lines {
			if strings.HasPrefix(l, "```") {
				continue
			}
			inner = append(inner, l)
		}
		s = strings.Join(inner, "\n")
	}
	// find first { ... }
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in response")
	}
	s = s[start : end+1]

	var d Decision
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		return nil, fmt.Errorf("JSON unmarshal: %w", err)
	}
	// validate bump
	switch d.Bump {
	case BumpNone, BumpPatch, BumpMinor, BumpMajor:
	default:
		d.Bump = BumpPatch
	}
	return &d, nil
}

func writeTempFile(content []byte) (string, error) {
	f, err := os.CreateTemp("", "publish-schema-*.json")
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.Write(content)
	return f.Name(), err
}
