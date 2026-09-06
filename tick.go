package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type tickResult struct {
	worked bool
	signal string
	status string
}

// cmdRun executes one tick for one agent.
func cmdRun(args []string) error {
	dir, rest, err := parseCompanyDir(args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return &cliErr{80, "usage: mago run <agent> [-C dir]"}
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	res, err := runTick(comp, rest[0])
	if err != nil {
		return err
	}
	if !res.worked {
		fmt.Println("no actionable tasks — nothing to do this tick")
	}
	return nil
}

// runTick: pick the active task, claim it, drive tau, reflect, write back.
func runTick(comp *Company, agentName string) (tickResult, error) {
	a, err := comp.loadAgent(agentName)
	if err != nil {
		return tickResult{}, err
	}
	applyModelOverrides(a)
	task, err := comp.tasks.PickActiveTask(agentName)
	if err != nil {
		return tickResult{}, err
	}
	if task == nil {
		return tickResult{worked: false, signal: "idle"}, nil
	}
	// Structural guard: a review-only agent must never sit on an issue-task — reviewers act on
	// PRs via the pull_request event path (reviewPR), not task routing. Bounce it (unassign +
	// reopen) so it re-routes to an implementer — no model call, no stall.
	if isReviewerRole(a) {
		fmt.Fprintf(os.Stderr, "[guard] %s is review-only — bouncing #%s for re-routing\n", agentName, task.ID)
		comp.tasks.Bounce(task)
		return tickResult{worked: true, signal: "working"}, nil
	}
	if err := comp.tasks.Claim(task, agentName); err != nil {
		return tickResult{}, err
	}
	ws := comp.workspaceFor(task)
	if repo := comp.taskRepo(task); repo != "" {
		w, err := comp.prepProjectWorkspace(task, repo) // fetch + branch from latest origin/<default>
		if err != nil {
			return tickResult{}, err
		}
		ws = w
	} else {
		ensureDir(ws)
	}
	fmt.Fprintf(os.Stderr, "=== mago tick: %s -> task #%s %q [%s] [%s/%s] ===\n",
		agentName, task.ID, task.Title, orDefault(task.Project, "default"), a.Provider, a.Model)
	content, err := runTau(ws, a, buildSystemPrompt(a), comp.buildBriefing(a, task))
	if err != nil {
		return tickResult{}, err
	}
	refl, err := parseReflection(content)
	if err != nil {
		// Recovery: ask once, with no tools, for just the reflection — this can't emit
		// the malformed tool-call markup that broke the parse.
		refl = comp.recoverReflection(a, task, ws)
	}
	if refl == nil {
		// Still nothing parseable. Don't crash the loop: the work is already on disk;
		// leave the task claimed and let the next tick re-ground and finish it.
		comp.writeRawFailure(a, task, content)
		fmt.Fprintf(os.Stderr, "warning: no parseable reflection this tick (raw saved); task #%s resumes next tick\n", task.ID)
		comp.pushState(fmt.Sprintf("tick %s on #%s (incomplete)", agentName, task.ID))
		return tickResult{worked: true, signal: "working"}, nil
	}
	// Guard: an implementer must not claim a project task "done" without an actual PR.
	// If the deliverable isn't shipped, keep it in-progress so the next tick finishes it.
	if refl.TaskStatus == "done" && !isReviewerRole(a) {
		if repo := comp.projectRepo(task.Project); task.Project != "" && repo != "" && !prShippedForTask(repo, task.ID) {
			fmt.Fprintf(os.Stderr, "[guard] #%s claimed done but no PR on %s for mago/task-%s — keeping in_progress\n", task.ID, repo, task.ID)
			refl.TaskStatus = "in_progress"
			refl.Next = "Open the PR: commit branch mago/task-" + task.ID +
				", `git push -u origin mago/task-" + task.ID + "`, then `gh pr create --fill --head mago/task-" + task.ID + "`."
		}
	}
	comp.writeBack(a, task, refl, content)
	comp.printRunResult(a, task, refl)
	comp.pushState(fmt.Sprintf("tick %s on #%s: %s", agentName, task.ID, oneLine(refl.Summary)))
	return tickResult{worked: true, signal: refl.CadenceSignal, status: refl.TaskStatus}, nil
}

// recoverReflection salvages a tick whose main output didn't parse, by asking the
// model (no tools) to emit just the reflection given the task and workspace state.
func (c *Company) recoverReflection(a *Agent, t *Task, ws string) *Reflection {
	if strings.TrimSpace(os.Getenv("MAGO_TEST_BAD_REFLECTION")) == "2" {
		return nil // test hook: simulate recovery ALSO failing -> self-heal path
	}
	prompt := "You just finished a work tick on this task:\nTITLE: " + t.Title +
		"\n\nThe workspace now contains:\n" + dirListing(ws) + "\n\nProduce your reflection now.\n\n" + reflectionInstruction
	out, err := tauComplete(a, prompt)
	if err != nil {
		return nil
	}
	r, err := parseReflection(out)
	if err != nil {
		return nil
	}
	fmt.Fprintln(os.Stderr, "[recovery] salvaged the reflection via a follow-up call")
	return r
}

// applyModelOverrides lets the smoke test switch provider/model via env without
// editing agent files (e.g. MAGO_PROVIDER=opencode-go).
func applyModelOverrides(a *Agent) {
	if p := strings.TrimSpace(os.Getenv("MAGO_PROVIDER")); p != "" {
		a.Provider = p
	}
	if m := strings.TrimSpace(os.Getenv("MAGO_MODEL")); m != "" {
		a.Model = m
	}
}

// providerKeyEnv maps a tau provider to the env var tau reads for its API key.
var providerKeyEnv = map[string]string{
	"opencode-go": "OPENCODE_API_KEY",
	"deepseek":    "DEEPSEEK_API_KEY",
	"openai":      "OPENAI_API_KEY",
}

// warnIfNoProviderKey alerts (BYOK) when no API key is configured for the resolved tau provider
// — neither in the env nor in ~/.config/tau/config.json. Without one, tau falls back to a
// rate-limited keyless/builtin path, which is what caused the throttling during batch runs.
// Also warns when a "flash" model variant is configured, since those often emit DSML tool-call
// markup instead of the required reflection JSON.
func warnIfNoProviderKey() {
	prov := strings.TrimSpace(os.Getenv("MAGO_PROVIDER")) // the override operators actually use (e.g. opencode-go)
	keyEnv, ok := providerKeyEnv[prov]
	if !ok {
		return
	}
	if m := strings.TrimSpace(os.Getenv("MAGO_MODEL")); strings.Contains(m, "flash") {
		fmt.Fprintf(os.Stderr, "[warn] model %q is a flash variant — these can emit DSML tool-call markup "+
			"instead of reflection JSON. Consider using a more reliable model (e.g. deepseek-v4).\n", m)
	}
	if strings.TrimSpace(os.Getenv(keyEnv)) != "" || tauConfigHasKey(prov) {
		return // key provided via env or the tau config file
	}
	fmt.Fprintf(os.Stderr, "[warn] no API key for provider %q — tau will use a rate-limited keyless/builtin "+
		"path. Set %s in the env, or add it to ~/.config/tau/config.json: \"keys\": {%q: \"...\"}.\n",
		prov, keyEnv, prov)
}

// tauConfigHasKey reports whether ~/.config/tau/config.json supplies a key for prov — a
// per-provider `keys[prov]` entry or the global `api_key` fallback.
func tauConfigHasKey(prov string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(home, ".config", "tau", "config.json"))
	if err != nil {
		return false
	}
	var c struct {
		APIKey string            `json:"api_key"`
		Keys   map[string]string `json:"keys"`
	}
	if json.Unmarshal(b, &c) != nil {
		return false
	}
	return c.APIKey != "" || c.Keys[prov] != ""
}
