package render_test

import (
	"io/fs"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/iesedobleac/iesedoble-ai/assets"
	"github.com/iesedobleac/iesedoble-ai/internal/render"
)

var values = map[string]string{
	"OPENCODE_TMP": "/tmp/opencode", "OPENCODE_TOOL_OUTPUT": "/data/tool-output", "DELIVERABLES": "/home/u/deliverables",
	"ANDROID_SDK": "/sdk", "GRADLE_CACHES": "/gradle/caches", "SKILLS_DIR": "/skills",
	"BIN_ENGRAM": "engram", "BIN_CODEGRAPH": "codegraph",
	"SPEC_LANGUAGE": "English", "EARS_EXAMPLE": "WHEN <event>, the system SHALL <response>", "DONE_WHEN": "Done when:",
}

var models = render.Models{"planner": "p/planner", "analyst": "p/analyst", "executor": "p/executor", "scout": "p/scout", "reviewer": "p/reviewer"}

func frontmatter(t *testing.T, text string) map[string]any {
	t.Helper()
	parts := strings.SplitN(text, "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		t.Fatalf("no frontmatter in:\n%.200s", text)
	}
	out := map[string]any{}
	if err := yaml.Unmarshal([]byte(parts[1]), &out); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, parts[1])
	}
	return out
}

func TestAgentsRender(t *testing.T) {
	agents, err := render.LoadAgents(assets.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 15 {
		t.Fatalf("expected 15 agents, got %d", len(agents))
	}
	servers := []string{"engram", "codegraph"}
	for _, a := range agents {
		oc, err := render.OpenCodeAgent(a, values, models)
		if err != nil {
			t.Fatalf("%s opencode: %v", a.Name, err)
		}
		fm := frontmatter(t, oc)
		perms, _ := fm["permissions"].([]any)
		if len(perms) != len(a.Rules) {
			t.Errorf("%s: %d permissions parsed, %d rules", a.Name, len(perms), len(a.Rules))
		}
		if fm["model"] != models[a.Profile] {
			t.Errorf("%s: model %v", a.Name, fm["model"])
		}
		claudeModels := render.Models{"planner": "opus", "analyst": "sonnet", "executor": "sonnet", "scout": "haiku", "reviewer": "opus"}
		cl, err := render.ClaudeAgent(a, agents, values, claudeModels, render.Efforts{"planner": "high", "scout": ""}, servers)
		if err != nil {
			t.Fatalf("%s claude: %v", a.Name, err)
		}
		cfm := frontmatter(t, cl)
		if cfm["name"] != a.Name || cfm["description"] == "" || cfm["tools"] == "" || cfm["model"] != claudeModels[a.Profile] {
			t.Errorf("%s: incomplete Claude frontmatter %v", a.Name, cfm)
		}
		// Only profiles with an effort get the field; empty keeps the session's.
		if want := map[string]any{"planner": "high"}[a.Profile]; cfm["effort"] != want {
			t.Errorf("%s (%s): effort %v, want %v", a.Name, a.Profile, cfm["effort"], want)
		}
		for _, text := range []string{oc, cl} {
			if strings.Contains(text, "@@") || strings.Contains(text, "{{") {
				t.Errorf("%s: unresolved marker", a.Name)
			}
		}
		if strings.Contains(cl, "`workdir`") || strings.Contains(cl, "sessionID") || strings.Contains(cl, "`execute`") {
			t.Errorf("%s: OpenCode-only wording in the Claude prompt", a.Name)
		}
	}
}

func TestClaudeTools(t *testing.T) {
	agents, _ := render.LoadAgents(assets.FS)
	byName := map[string]render.Agent{}
	for _, a := range agents {
		byName[a.Name] = a
	}
	worker := strings.Join(render.ClaudeTools(byName["android-worker"], agents, []string{"codegraph"}), ", ")
	if worker != "Read, Grep, Glob, Edit, Write, Bash, mcp__codegraph__*" {
		t.Errorf("android-worker tools: %s", worker)
	}
	explore := strings.Join(render.ClaudeTools(byName["android-explore"], agents, []string{"codegraph", "firebase"}), ", ")
	if explore != "Read, Grep, Glob, Bash, mcp__codegraph__*, mcp__firebase__*" {
		t.Errorf("android-explore tools: %s", explore)
	}
	research := strings.Join(render.ClaudeTools(byName["android-research"], agents, []string{"codegraph", "context7"}), ", ")
	if research != "WebFetch, WebSearch, mcp__context7__*" {
		t.Errorf("android-research tools: %s", research)
	}
	orch := strings.Join(render.ClaudeTools(byName["sdd-orchestrator"], agents, []string{"codegraph", "engram"}), ", ")
	for _, want := range []string{"Agent(android-device, android-explore, android-research, android-review, android-worker, sdd-apply,", "mcp__engram__mem_search", "AskUserQuestion"} {
		if !strings.Contains(orch, want) {
			t.Errorf("sdd-orchestrator tools missing %q: %s", want, orch)
		}
	}
}

func TestSkillsAndRulesRender(t *testing.T) {
	for _, target := range []string{"opencode", "claude"} {
		err := fs.WalkDir(assets.FS, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") || strings.HasPrefix(p, "agents/") {
				return err
			}
			data, _ := fs.ReadFile(assets.FS, p)
			if _, err := render.Template(string(data), target, values); err != nil {
				t.Errorf("%s (%s): %v", p, target, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
