package publish

// EngineType identifies which LLM backend to use.
type EngineType string

const (
	EngineOllama EngineType = "ollama"
	EngineClaude EngineType = "claude"
	EngineCodex  EngineType = "codex"
)

// ProjectType identifies the language/build system of a repo.
type ProjectType string

const (
	TypeWeb     ProjectType = "web"
	TypeGo      ProjectType = "go"
	TypePython  ProjectType = "python"
	TypeText    ProjectType = "text"
	TypeUnknown ProjectType = "unknown"
)

// ChangeScope classifies what kind of files changed.
type ChangeScope string

const (
	ScopeDocs  ChangeScope = "docs"
	ScopeBuild ChangeScope = "build"
	ScopeCode  ChangeScope = "code"
)

// BumpLevel is the semver bump recommended by the LLM (or clamped by scope).
type BumpLevel string

const (
	BumpNone  BumpLevel = "none"
	BumpPatch BumpLevel = "patch"
	BumpMinor BumpLevel = "minor"
	BumpMajor BumpLevel = "major"
)

// Decision is the LLM output for a single repo.
type Decision struct {
	Bump    BumpLevel `json:"bump"`
	Message string    `json:"message"`
	Reason  string    `json:"reason"`
}

// K8SPlan holds one planned k8s image update: every manifest referencing the
// image (the service deployment + any worker deployments) moves to the same
// version. YAMLs[0] is the primary manifest (env vars are injected only there).
type K8SPlan struct {
	Image   string
	YAMLs   []string
	Version string
	RepoDir string
}

// LLMSchema is the JSON schema for ollama structured output.
const LLMSchema = `{"type":"object","properties":{"reason":{"type":"string"},"bump":{"type":"string","enum":["none","major","minor","patch"]},"message":{"type":"string"}},"required":["reason","bump","message"]}`

// CodexSchema adds additionalProperties:false required by OpenAI strict output.
const CodexSchema = `{"type":"object","additionalProperties":false,"properties":{"reason":{"type":"string"},"bump":{"type":"string","enum":["none","major","minor","patch"]},"message":{"type":"string"}},"required":["reason","bump","message"]}`

// LLMSystem is the system prompt sent to every LLM engine.
const LLMSystem = `You are a release engineer for the "Cross Exchange" microservice platform. You read a git change (FACTS + DIFF) and output a release decision as JSON with fields in this order: reason, bump, message.

First write "reason": 1-2 sentences naming the concrete changes (use the NEW/DELETED files and NEW ENV VARS in FACTS).

FACTS includes a mechanically-computed "CHANGE SCOPE" you MUST respect:
  - CHANGE SCOPE: docs  -> the bump is NONE (only docs/examples/text changed).
  - CHANGE SCOPE: build -> the bump is at most PATCH (only Dockerfile/.env/deps/CI changed).
  - CHANGE SCOPE: code  -> use the decision procedure below.

Then choose "bump" by applying this decision procedure IN ORDER, stopping at the first match:
  0. NONE  — ships no behaviour change: only docs/README, comments, example or sample code (an examples/ directory counts even if it adds many files), tests, .gitignore/.dockerignore, or pure formatting. Documentation that merely DESCRIBES an endpoint is still NONE -- writing API docs is not implementing the endpoint. Version will not change and the service will not be redeployed.
  1. MAJOR — a backward-INCOMPATIBLE change to the PUBLIC contract: removing/renaming an existing HTTP endpoint, changing an existing request/response field name/type/meaning, or removing a previously-required config value. (An internal file split/rename like repo.go -> user.go is NOT this.)
  2. MINOR — a new backward-COMPATIBLE capability implemented IN CODE: a new HTTP endpoint/route handler, a new email/notification, a new integration/service, OR a new env var that is listed under "NEW ENV VARS" in FACTS. New feature files (e.g. reset_password.go) that expose new behaviour count here. An env var that only appears inside a .env file (and is NOT under FACTS NEW ENV VARS) does NOT count.
  3. PATCH — a bug fix, an internal-only refactor/reformat, OR a change only to build/CI tooling (Dockerfile, .env/.env.* files, dependency pins): it rebuilds the image but adds no user-facing feature.
Apply rule 0 FIRST; it takes priority even when many files changed. Your "bump" MUST agree with your "reason".

Then write "message": one line in the imperative mood (e.g. "Add ...", "Fix ...", "Update ..."), <=72 characters, NO trailing period, no "feat:"/"fix:" prefix. Describe ONLY changes in THIS diff (anchor on FACTS); do NOT mention pre-existing features that merely appear in nearby context. Lead with the most important user-facing change.`
