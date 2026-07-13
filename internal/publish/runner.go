package publish

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner orchestrates a full publish run.
type Runner struct {
	cfg *Config
	ui  *UI
}

func NewRunner(cfg *Config, ui *UI) *Runner {
	return &Runner{cfg: cfg, ui: ui}
}

// stamp prints the wall-clock time a phase starts so it shows up in logs/locks.
func (r *Runner) stamp(phase string) {
	r.ui.Say(r.ui.D(fmt.Sprintf("[%s] %s started", time.Now().Format("2006-01-02 15:04:05"), phase)))
}

// RunAll publishes across every environment: it first build-checks every changed
// service, then deploys to dev (bump + commit + push repos, update dev k8s), and
// only if that succeeds promotes the same versions to prod.
func (r *Runner) RunAll() error {
	r.stamp("all")
	r.ui.Hdr(fmt.Sprintf("publish — mode: %s  %s", r.ui.B("all"),
		r.ui.D("(check → dev → prod)")))

	r.ui.Hdr(r.ui.B("[1/3] check") + r.ui.D(" — build every changed service"))
	if err := r.preflightCheck(); err != nil {
		return fmt.Errorf("check failed, nothing deployed: %w", err)
	}

	r.ui.Hdr(r.ui.B("[2/3] dev") + r.ui.D(" — commit + push repos, deploy dev"))
	if err := r.RunDev(); err != nil {
		return fmt.Errorf("dev deploy failed, NOT promoting to prod: %w", err)
	}

	if r.cfg.DryRun {
		r.ui.Warn("DRY RUN — prod promotion skipped; re-run without --dry-run to publish.")
		return nil
	}

	r.ui.Hdr(r.ui.B("[3/3] prod") + r.ui.D(" — promote versions to prod"))
	if err := r.RunProd(); err != nil {
		return fmt.Errorf("prod deploy failed: %w", err)
	}
	return nil
}

// preflightCheck builds every changed, buildable service and returns an error
// naming any that fail to compile, so a broken service never reaches deploy.
func (r *Runner) preflightCheck() error {
	var failed []string
	checked := 0
	for _, s := range DiscoverServices(r.cfg) {
		ResetDSStore(s.GitDir)
		if !serviceDirty(s.Dir) {
			continue
		}
		t := DetectType(s.Dir)
		if !Buildable(t) {
			continue
		}
		checked++
		fmt.Fprintf(os.Stderr, "%s        building %s (%s)…%s\n", r.ui.Dim, s.Name, t, r.ui.Reset)
		if err := BuildCheck(s.Dir, t); err != nil {
			r.ui.Err(fmt.Sprintf("%s: build failed: %v", s.Name, err))
			failed = append(failed, s.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d service(s) do not build: %s", len(failed), strings.Join(failed, ", "))
	}
	r.ui.Say(r.ui.Green + fmt.Sprintf("  all %d service(s) build", checked) + r.ui.Reset)
	return nil
}

// RunProd promotes current repo versions into the prod k8s manifests.
func (r *Runner) RunProd() error {
	r.stamp("prod")
	envDir := r.cfg.ProdEnvDir()
	r.ui.Hdr(fmt.Sprintf("publish — mode: %s  (k8s env: %s, apply: %s)",
		r.ui.B("prod"), filepath.Base(envDir), r.fmtApply()))
	if r.cfg.DryRun {
		r.ui.Warn("DRY RUN — no files will be edited, nothing committed or pushed.")
	}
	if r.cfg.FolderFilter != "" {
		r.ui.Say(fmt.Sprintf("  %s: %s %s", r.ui.B("folder"), r.cfg.FolderFilter,
			r.ui.D("(only this repo will be processed)")))
	}

	i := 0
	for _, s := range DiscoverServices(r.cfg) {
		if s.TagRepo || s.Image == "" || !isGitRepo(s.GitDir) {
			continue
		}
		yamlPaths := FindK8sYAMLs(s.Image, envDir)
		if len(yamlPaths) == 0 {
			continue
		}
		i++
		t := DetectType(s.Dir)
		cur := ReadVersion(s.Dir, t)
		r.ui.Hdr(fmt.Sprintf("[%d] %s", i, s.Name))
		r.ui.Say(fmt.Sprintf("  version (from source): %s", r.ui.B(cur)))
		for _, yamlPath := range yamlPaths {
			r.ui.Say(fmt.Sprintf("  image tag (%s): %s", filepath.Base(yamlPath),
				UpdateK8sImage(yamlPath, s.Image, cur, !r.cfg.DryRun)))
		}
		for _, added := range InjectEnvVars(yamlPaths, s.Dir, !r.cfg.DryRun) {
			r.ui.Say(fmt.Sprintf("  + %s", added))
		}
	}

	if !r.cfg.DryRun {
		r.ui.Hdr(fmt.Sprintf("Commit & push k8s (%s)", envDir))
		if err := CommitAndPushK8s(r.cfg.K8SDir, envDir, "prod: promote service versions"); err != nil {
			return fmt.Errorf("k8s push: %w", err)
		}
	}
	return nil
}

// RunDev runs the per-repo LLM decision, bumps versions, commits, then updates k8s.
func (r *Runner) RunDev() error {
	r.stamp("dev")
	envDir := r.cfg.DevEnvDir()
	r.ui.Hdr(fmt.Sprintf("publish — mode: %s  (k8s env: %s, apply: %s)",
		r.ui.B("dev"), filepath.Base(envDir), r.fmtApply()))
	if r.cfg.DryRun {
		r.ui.Warn("DRY RUN — no files will be edited, nothing committed or pushed.")
	}
	if r.cfg.FolderFilter != "" {
		r.ui.Say(fmt.Sprintf("  %s: %s %s", r.ui.B("folder"), r.cfg.FolderFilter,
			r.ui.D("(only this repo will be processed)")))
	}
	promptLabel := r.cfg.LoadPrompt()
	r.ui.Say(r.ui.D(fmt.Sprintf("model: %s   prompt: %s", r.cfg.ModelID, promptLabel)))

	services := DiscoverServices(r.cfg)
	var dirty []Service
	for _, s := range services {
		ResetDSStore(s.GitDir)
		if serviceDirty(s.Dir) {
			dirty = append(dirty, s)
		}
	}
	names := make([]string, len(dirty))
	for i, s := range dirty {
		names[i] = s.Name
	}
	r.ui.Hdr(fmt.Sprintf("Found %s service(s) with changes  %s",
		r.ui.B(fmt.Sprint(len(dirty))), r.ui.D(strings.Join(names, " "))))

	var k8sPlan []K8SPlan
	type pendingCommit struct{ subdir, message string }
	commits := map[string][]pendingCommit{}
	var gitOrder []string
	start := time.Now()

	for idx, s := range dirty {
		repoDir := s.Dir
		t := DetectType(repoDir)

		var cur string
		if s.TagRepo {
			cur = TagVersion(repoDir)
		} else {
			cur = ReadVersion(repoDir, t)
		}
		scope := ComputeChangeScope(repoDir)
		nChanged := countDirtyFiles(repoDir)

		fmt.Fprintf(os.Stderr, "%s[%d/%d]%s %-22s %sanalyzing diff (%s, scope=%s)…%s\n",
			r.ui.Bold+r.ui.Cyan, idx+1, len(dirty), r.ui.Reset,
			s.Name, r.ui.Dim, r.cfg.ModelID, scope, r.ui.Reset)

		t0 := time.Now()
		addedEnvs := AddedEnvVars(repoDir)
		ctx, _ := BuildCtx(repoDir, addedEnvs)
		result := Decide(r.cfg, s.Name, cur, ctx)
		dt := time.Since(t0).Round(time.Second)

		var tlabel string
		switch result.Source {
		case CacheHit:
			tlabel = "cached"
		case CacheDown:
			tlabel = r.ui.Red + "LLM unavailable" + r.ui.Reset + r.ui.Dim
		default:
			tlabel = fmt.Sprintf("%s %s", r.cfg.ModelID, dt)
		}

		// deterministic scope clamp
		bump := result.Bump
		clamp := ""
		switch scope {
		case ScopeDocs:
			if bump != BumpNone {
				clamp = r.ui.D(fmt.Sprintf(" (scope=docs: model said %s)", bump))
				bump = BumpNone
			}
		case ScopeBuild:
			if bump == BumpMajor || bump == BumpMinor {
				clamp = r.ui.D(fmt.Sprintf(" (scope=build: model said %s)", bump))
				bump = BumpPatch
			}
		}

		newVer := BumpVersion(cur, bump)

		r.ui.Hdr(fmt.Sprintf("[%d/%d] %s  %s", idx+1, len(dirty), s.Name,
			r.ui.D(fmt.Sprintf("(%s, %d changed, scope=%s, %s)", t, nChanged, scope, tlabel))))
		r.ui.Say(fmt.Sprintf("  %s  %s", r.ui.B("reason:"), result.Reason))
		r.ui.Say(fmt.Sprintf("  %s  %s%s    %s", r.ui.B("bump:"), bump, clamp,
			r.ui.D(fmt.Sprintf("%s -> %s", cur, newVer))))
		r.ui.Say(fmt.Sprintf("  %s %s", r.ui.B("message:"), result.Message))

		// build gate: never commit a service that doesn't compile
		if Buildable(t) {
			fmt.Fprintf(os.Stderr, "%s        building %s (%s)…%s\n", r.ui.Dim, s.Name, t, r.ui.Reset)
			t0 := time.Now()
			if err := BuildCheck(repoDir, t); err != nil {
				r.ui.Err(fmt.Sprintf("%s: build failed, service skipped: %v", s.Name, err))
				continue
			}
			r.ui.Say("  " + r.ui.Green + fmt.Sprintf("build ok (%s)", time.Since(t0).Round(time.Second)) + r.ui.Reset)
		}

		// tag repos publish a GitHub git tag, not a k8s deploy
		if s.TagRepo {
			r.printTagPlan(s.Name, bump, newVer, result.Message, repoDir)
			if !r.cfg.DryRun {
				if err := r.applyTagRepo(repoDir, s.Name, bump, newVer, result.Message); err != nil {
					r.ui.Err(fmt.Sprintf("%s: %v", s.Name, err))
				}
			}
			continue
		}

		// service repo: collect k8s plan (every manifest referencing the image)
		var yamlPaths []string
		if s.Image != "" {
			yamlPaths = FindK8sYAMLs(s.Image, envDir)
		}
		primaryYAML := ""
		if len(yamlPaths) > 0 {
			primaryYAML = yamlPaths[0]
		}

		if newEnvs := NewEnvVars(repoDir, yamlPaths); len(newEnvs) > 0 {
			tgt := "no k8s manifest in " + filepath.Base(envDir)
			if primaryYAML != "" {
				tgt = "-> " + filepath.Base(primaryYAML)
			}
			r.ui.Say(fmt.Sprintf("  %s (%s):", r.ui.B("new env vars"), tgt))
			for _, k := range newEnvs {
				r.ui.Say(fmt.Sprintf("      %s%s%s=%q", r.ui.Green, k, r.ui.Reset, EnvValue(repoDir, k)))
			}
		}

		if len(yamlPaths) > 0 && bump != BumpNone {
			k8sPlan = append(k8sPlan, K8SPlan{Image: s.Image, YAMLs: yamlPaths, Version: newVer, RepoDir: repoDir})
		}

		if r.cfg.DryRun {
			if bump == BumpNone {
				r.ui.Say(r.ui.D(fmt.Sprintf("  (dry-run: would commit %q — no version bump)", result.Message)))
			} else {
				r.ui.Say(r.ui.D(fmt.Sprintf("  (dry-run: would commit %q and write version %s)", result.Message, newVer)))
			}
			continue
		}

		if bump != BumpNone {
			if err := WriteVersion(repoDir, t, newVer); err != nil {
				r.ui.Err(fmt.Sprintf("write version: %v", err))
			}
		}
		if _, ok := commits[s.GitDir]; !ok {
			gitOrder = append(gitOrder, s.GitDir)
		}
		commits[s.GitDir] = append(commits[s.GitDir], pendingCommit{filepath.Base(repoDir), result.Message})
	}

	// one commit per git repo — single-repo projects fold all their services
	// into a single commit; multi-repo projects get one commit per repo.
	for _, gd := range gitOrder {
		var parts []string
		for _, c := range commits[gd] {
			if len(commits[gd]) == 1 {
				parts = append(parts, c.message)
			} else {
				parts = append(parts, c.subdir+": "+c.message)
			}
		}
		msg := strings.Join(parts, "\n")
		fmt.Fprintf(os.Stderr, "%s        committing & pushing %s…%s\n", r.ui.Dim, filepath.Base(gd), r.ui.Reset)
		if err := GitAddCommitPush(gd, msg); err != nil {
			r.ui.Err(fmt.Sprintf("commit/push %s: %v", filepath.Base(gd), err))
		} else {
			r.ui.Say("  " + r.ui.Green + "committed & pushed " + filepath.Base(gd) + r.ui.Reset)
		}
	}

	// k8s manifest step
	r.ui.Hdr(fmt.Sprintf("Kubernetes manifests (%s)", filepath.Base(envDir)))
	if len(k8sPlan) == 0 {
		r.ui.Say(r.ui.D("  no image tags to change"))
	} else {
		for _, plan := range k8sPlan {
			for _, yamlPath := range plan.YAMLs {
				r.ui.Say(fmt.Sprintf("  %s: %s",
					r.ui.B(filepath.Base(yamlPath)),
					UpdateK8sImage(yamlPath, plan.Image, plan.Version, !r.cfg.DryRun)))
			}
			for _, added := range InjectEnvVars(plan.YAMLs, plan.RepoDir, !r.cfg.DryRun) {
				r.ui.Say(fmt.Sprintf("  + %s", added))
			}
		}
	}

	if r.cfg.DryRun {
		r.ui.Warn("DRY RUN complete — re-run without --dry-run to actually publish.")
	} else {
		r.ui.Hdr("Commit & push k8s")
		if err := CommitAndPushK8s(r.cfg.K8SDir, envDir, "dev: bump service image versions"); err != nil {
			return fmt.Errorf("k8s push: %w", err)
		}
		r.ui.Say(r.ui.Green + "k8s pushed — kubectl apply will be triggered" + r.ui.Reset)
	}

	r.ui.Hdr(fmt.Sprintf("Done in %s  (%d service(s) processed)",
		r.ui.B(fmtDuration(time.Since(start))), len(dirty)))
	return nil
}

func (r *Runner) printTagPlan(repo string, bump BumpLevel, newVer, msg, repoDir string) {
	if bump == BumpNone {
		r.ui.Say(fmt.Sprintf("  %s: commit only %s", r.ui.B("publish"),
			r.ui.D("(no version bump → no tag)")))
		if r.cfg.DryRun {
			r.ui.Say(r.ui.D(fmt.Sprintf("  (dry-run: would commit %q — no tag)", msg)))
		}
	} else {
		r.ui.Say(fmt.Sprintf("  %s: GitHub tag %s%s", r.ui.B("publish"),
			r.ui.B("v"+newVer), r.ui.D(fmt.Sprintf("  (%s)", RemoteURL(repoDir)))))
		if r.cfg.DryRun {
			r.ui.Say(r.ui.D(fmt.Sprintf("  (dry-run: would commit %q, then tag & push v%s to GitHub)", msg, newVer)))
		}
	}
}

func (r *Runner) applyTagRepo(repoDir, repo string, bump BumpLevel, newVer, msg string) error {
	fmt.Fprintf(os.Stderr, "%s        committing, tagging & pushing %s…%s\n", r.ui.Dim, repo, r.ui.Reset)
	ResetDSStore(repoDir)
	if err := GitAddCommitPush(repoDir, msg); err != nil {
		return err
	}
	if bump != BumpNone {
		if err := TagAndPush(repoDir, "v"+newVer, msg); err != nil {
			return err
		}
		GHReleaseCreate(repoDir, "v"+newVer, msg)
		r.ui.Say(r.ui.Green + fmt.Sprintf("  published tag v%s", newVer) + r.ui.Reset)
	} else {
		r.ui.Say(r.ui.Green + "  committed & pushed" + r.ui.Reset)
	}
	return nil
}

// serviceDirty reports whether the service subtree at dir has uncommitted
// changes (ignoring .DS_Store), OR if any files in the service have been
// committed but not yet deployed (checked via git diff from the last deployed tag).
// Scoped with "-- ." so that, in a single-repo project, each service's dirtiness
// is judged independently.
func serviceDirty(dir string) bool {
	// Check for uncommitted changes
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--", ".").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" && !strings.Contains(line, ".DS_Store") {
			return true
		}
	}

	// Also check if there are committed changes since the last deployed version.
	// This handles the case where versions were bumped and committed but images
	// haven't been built/pushed yet. We check if HEAD has changes relative to origin.
	out, _ = exec.Command("git", "-C", dir, "rev-list", "--count", "origin/master..HEAD", "--", ".").Output()
	commitCount := strings.TrimSpace(string(out))
	if commitCount != "" && commitCount != "0" {
		// HEAD is ahead of origin/master, meaning new commits exist
		return true
	}

	return false
}

func (r *Runner) fmtApply() string {
	if r.cfg.DryRun {
		return "no"
	}
	return "yes"
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func countDirtyFiles(dir string) int {
	out, _ := exec.Command("git", "-C", dir, "status", "--porcelain", "--", ".").Output()
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" && !strings.Contains(line, ".DS_Store") {
			count++
		}
	}
	return count
}

func fmtDuration(d time.Duration) string {
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", mins, secs)
}
