# publish

Cross Exchange release tool — rewrite of `publish.sh` in Go.

Walks every microservice repo of a project, asks a local (or cloud) LLM to write
a commit message and choose the semver bump from the diff, bumps the version in the right
place for each project type, commits + pushes, then updates the Kubernetes deploy manifests
and pushes those too.

It handles two project layouts (auto-detected):

- **Multi-repo** (e.g. `projects/cross`): the project folder is a *container* of many
  independent git repos, one per service. Each child repo is published on its own.
- **Single-repo** (e.g. `projects/book-reader`): the project folder is *itself* one git
  repo; each depth-1 subdirectory that has a `Dockerfile` + a version source (e.g.
  `backend/`, `frontend/`) is a service. The services are versioned/bumped
  independently but share **one commit** in the repo.

The k8s env dirs are derived from the project folder name: `cross` → `dev-cross` / `cross`,
`book-reader` → `dev-book-reader` / `book-reader`. Service images in single-repo mode are
matched as `<project>/<subdir>` (e.g. `book-reader/backend`).

A few projects deploy into a differently-named env dir; these are mapped in
`k8sEnvDirs` (`internal/publish/service.go`): `leia` and `tts-2` → `ai-features` /
`ai-features` (same dir for dev and prod).

Single-repo projects whose folder name differs from the deployed image are mapped
in `singleRepoImages` (`internal/publish/service.go`): `tts-2` → `tts-server`
(ingress host `tts.kevyn.com.br`).

---

## Build

Requires Go 1.25+.

```bash
# from the publish directory
go build -o bin/publish .
```

Install once so `publish` is on your `$PATH` (symlink, so future rebuilds are picked up automatically):

```bash
ln -sf "$PWD/bin/publish" ~/.local/bin/publish
```

To update after code changes, just rebuild:

```bash
cd ~/WebstormProjects/publish
go build -o bin/publish .
```

---

## Usage

```
publish [command] [flags]

Commands:
  dev         Bump + commit + push each changed repo, then update k8s dev-cross/ (default)
  prod        Promote current repo versions into k8s cross/ (prod) and push
  all         Build-check every changed repo, deploy dev, then promote to prod if dev succeeds
  clean       Wipe the decision cache
  completion  Generate shell completion script (zsh, bash, fish, powershell)
  help        Help about any command

Global flags:
  -f, --folder string   Restrict to a single repo; k8s manifests still updated for that repo
  -l, --lazy            Thorough local model (gemma4) — slower but more nuanced
  -s, --smart           Smart but slow: Claude Code headless (Haiku) on your subscription
  -c, --codex           Smart and fast: codex exec (gpt-5.5, structured JSON)
  -d, --dry-run         Preview only — no edits, commits or pushes
```

### Common invocations

```bash
publish                               # dev deploy, default engine (qwen2.5:3b)
publish --dry-run                     # preview dev without committing
publish dev -d                        # same — short flag
publish -f user-api                   # dev deploy only user-api
publish -f shared                     # publish the shared library repo (git tag)
publish -d -f user-api                # preview only user-api
publish -l                            # dev deploy with the thorough gemma model
publish -f user-api -l                # single repo + gemma
publish -f user-api -c                # single repo + codex engine
publish prod                          # promote current versions to prod k8s
publish prod --dry-run                # preview prod promotion
publish prod -f user-api              # prod promote only user-api
publish all                           # check, deploy dev, then promote to prod
publish all --dry-run                 # preview dev (prod promotion skipped)
publish clean                         # wipe the decision cache
```

---

## Shell completion

Cobra generates the completion script automatically, including tab-completion for
`--folder` that shows all known repo names.

```bash
# zsh — install once
publish-go completion zsh > ~/.zsh/completions/_publish-go
rm -f ~/.zcompdump*

# bash
publish-go completion bash > /usr/local/etc/bash_completion.d/publish-go

# fish
publish-go completion fish > ~/.config/fish/completions/publish-go.fish
```

---

## Engines

Three LLM engines are available, selected by flag. All produce the same
`{bump, message, reason}` JSON output.

| Flag | Engine | Speed | Notes |
|------|--------|-------|-------|
| *(none)* | ollama `qwen2.5:3b` | ~3 s/repo | Default. Requires `ollama serve`. Auto-started if not running. Uses `decision.qwen1.txt` prompt if present. |
| `--lazy` / `-l` | ollama `gemma4` | ~10 s/repo | Slower but more thorough. Uses the built-in prompt. |
| `--smart` / `-s` | Claude Code headless (Haiku) | ~40 s/repo | Runs on your Claude subscription via `claude -p`. |
| `--codex` / `-c` | `codex exec` gpt-5.5 | ~6 s/repo | Structured JSON output. Requires the codex CLI logged in. |

Override the ollama model with `OLLAMA_MODEL=...`. Override the lazy model with
`PUBLISH_LAZY_MODEL=...`.

---

## Decision cache

Each service's `{bump, message}` decision is cached in `$PROJECTS_DIR/.publish-cache/<repo>.cache`
(multi-repo projects) or under `$TMPDIR/publish-cache/<project>/` (single-repo projects, so a
blanket `git add -A` never commits it),
keyed by a SHA-256 hash of the model ID + prompt + diff context. A dry-run fills the
cache; the following real `dev` run reuses it so what you reviewed is exactly what gets
applied. Editing the code auto-invalidates the cache entry.

Set `PUBLISH_NO_CACHE=1` to always re-query. Run `publish-go clean` to wipe the whole cache.

The cache file format is compatible with the original `publish.sh` cache.

---

## Project type detection

| Type | Detection | Version source(s) | Version write targets |
|------|-----------|-------------------|-----------------------|
| **Go** | `go.mod` or `cmd/*/main.go` with `@version` | `cmd/*/main.go`, `docs/swagger.yaml`, `docs/swagger.json`, `docs/docs.go` | Same files |
| **Web** | `package.json` | `"version"` field in `package.json` | `package.json` |
| **Python** | `setup.py` or `*/version.py` | `*/version.py` | `*/version.py` |
| **Text** | `version.txt` | `version.txt` | `version.txt` |

Version across multiple sources is resolved to the maximum (handles drift between
swagger files and main.go).

---

## Change scope guardrail

The LLM decides the bump, but the mechanically-computed **change scope** acts as a
hard clamp — the LLM cannot over-bump a docs or build-only change:

| Scope | Files changed | Max bump allowed |
|-------|--------------|------------------|
| `docs` | README, `.md`, examples, `.txt` | `none` |
| `build` | Dockerfile, `.env`, go.sum, CI | `patch` |
| `code` | Everything else | any |

---

## Env var detection and injection

The tool greps the repo's source code for env-var read patterns
(`os.Getenv`, `getEnv`, `process.env.X`, etc.) and compares them against what the
k8s manifest already declares. Any missing keys are injected into the manifest's
`env:` block, with values sourced from the repo's `.env` file or `getEnv("KEY","default")`
call sites.

Manifests with no `env:` block (static web app deployments) are skipped.

---

## Library repos (git tags)

Repos in `TAG_REPOS` (default: `shared`) are published as a GitHub git tag + release
instead of a k8s deployment:

1. `git add -A && git commit -m <message>`
2. `git tag -a v<new> -m <message> && git push origin v<new>`
3. `gh release create v<new> …` (best-effort)

`bump=none` → commit only, no tag.

---

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `PROJECTS_DIR` | current folder if it's a git repo or contains git repos, else `~/projects/cross` | Project folder to publish |
| `K8S_DIR` | `~/WebstormProjects/k8s` | Root of the k8s manifests repo |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama API endpoint |
| `OLLAMA_MODEL` | `qwen2.5:3b` | Default ollama model |
| `PUBLISH_LAZY_MODEL` | `gemma4` | Model used with `--lazy` |
| `CLAUDE_MODEL` | `haiku` | Claude model for `--smart` |
| `CODEX_MODEL` | `gpt-5.5` | Codex model for `--codex` |
| `CODEX_REASONING` | `low` | Codex reasoning effort |
| `PUBLISH_CACHE_DIR` | `$PROJECTS_DIR/.publish-cache` (multi-repo) or `$TMPDIR/publish-cache/<project>` (single-repo) | Decision cache directory |
| `PROMPTS_DIR` | `$PROJECTS_DIR/prompts` | Directory for prompt overrides |
| `PUBLISH_PROMPT_FILE` | *(none)* | Explicit prompt file override |
| `PUBLISH_NO_CACHE` | `0` | Set to `1` to disable cache reads |
| `TAG_REPOS` | `shared` | Space-separated list of tag-only repos |

---

## Architecture

```
publish/
├── main.go                      # entry point
├── cmd/
│   └── root.go                  # cobra CLI — commands, flags, completion
└── internal/publish/
    ├── types.go                 # EngineType, BumpLevel, Decision, K8SPlan, LLM prompts
    ├── config.go                # Config struct, repo list, image name map, prompt loading
    ├── semver.go                # NormSemver, BumpVersion, SemverLess
    ├── version.go               # DetectType, ReadVersion, WriteVersion (Go/web/Python)
    ├── scope.go                 # FileClass, ScopeFromFiles, ComputeChangeScope, BuildCtx
    ├── env.go                   # CodeEnvsFS/HEAD, NewEnvVars, EnvValue
    ├── cache.go                 # SHA-256 keyed file cache
    ├── llm.go                   # Ollama (HTTP), Claude (CLI exec), Codex (CLI exec), EnsureOllama
    ├── k8s.go                   # FindK8sYAML, UpdateK8sImage, InjectEnvVars, CommitAndPushK8s
    ├── git.go                   # IsDirty, GitAddCommitPush, TagAndPush, GHReleaseCreate
    ├── runner.go                # RunDev, RunProd orchestration
    └── ui.go                    # Coloured terminal output helpers
```

---

## Differences from publish.sh

| Concern | `publish.sh` | `publish-go` |
|---------|-------------|--------------|
| `-f` flag | dual-purpose lookahead hack | proper `--folder` flag; engine flags are separate |
| dry-run | second positional arg (`dev dry-run`) | global `-d`/`--dry-run` flag |
| shell completion | hand-written zsh script | cobra auto-generated; `--folder` tab-completes repo names |
| bash 3.2 compat | required (no assoc arrays) | not a constraint |
| cache format | `sha256(model+prompt+ctx) \| bump\|msg\|reason` | identical — cross-compatible |
| test surface | none | each package is independently testable |
