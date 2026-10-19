// Package render turns the neutral sources into each assistant's files.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Target describes how templates render for one assistant.
type Target struct {
	Name   string
	Blocks []string          // block names that are kept, e.g. {{#opencode}}
	Tokens map[string]string // inline {{token}} values
}

// Targets are the supported assistants.
var Targets = map[string]Target{
	"opencode": {
		Name:   "opencode",
		Blocks: []string{"opencode"},
		Tokens: map[string]string{
			"read": "`read`", "grep": "`grep`", "glob": "`glob`", "edit": "`edit`", "webfetch": "`webfetch`",
			"oldString": "`oldString`", "skill_tool": "the skill tool", "workdir": "`workdir`",
			"git_cwd": "using the shell tool's `workdir` parameter", "tmp_label": "OpenCode", "product": "OpenCode",
			"skills_user_dir": "~/.config/opencode/skills", "skills_repo_dir": ".opencode/skills", "mcp_example": "",
		},
	},
	"claude": {
		Name:   "claude",
		Blocks: []string{"other", "claude"},
		Tokens: map[string]string{
			"read": "`Read`", "grep": "`Grep`", "glob": "`Glob`", "edit": "`Edit`", "webfetch": "`WebFetch`",
			"oldString": "`old_string`", "skill_tool": "the `Skill` tool", "workdir": "working directory",
			"git_cwd": "from the repository root (start the command with `cd <dir> &&`)", "tmp_label": "shared",
			"product": "Claude Code", "skills_user_dir": "~/.claude/skills", "skills_repo_dir": ".claude/skills",
			"mcp_example": "`mcp__codegraph__codegraph_explore` and `mcp__engram__mem_search`",
		},
	},
}

var (
	blockOpen   = regexp.MustCompile(`\{\{#([a-z]+)\}\}`)
	tokenRe     = regexp.MustCompile(`\{\{([a-zA-Z_]+)\}\}`)
	placeholder = regexp.MustCompile(`@@([A-Z_]+)@@`)
	leftover    = regexp.MustCompile(`\{\{[^}]*\}\}`)
)

// Template renders blocks, tokens and @@PLACEHOLDERS@@ for a target.
func Template(text, target string, values map[string]string) (string, error) {
	t, ok := Targets[target]
	if !ok {
		return "", fmt.Errorf("unknown target %q", target)
	}
	active := map[string]bool{}
	for _, b := range t.Blocks {
		active[b] = true
	}
	out, err := blocks(text, active)
	if err != nil {
		return "", err
	}
	tokens := map[string]string{}
	for k, v := range t.Tokens {
		tokens[k] = v
	}
	tokens["repo"] = "<repository root>"
	var tokenErr error
	out = tokenRe.ReplaceAllStringFunc(out, func(m string) string {
		key := tokenRe.FindStringSubmatch(m)[1]
		v, ok := tokens[key]
		if !ok {
			tokenErr = fmt.Errorf("unknown token %s", m)
		}
		return v
	})
	if tokenErr != nil {
		return "", tokenErr
	}
	if out, err = Fill(out, values); err != nil {
		return "", err
	}
	if m := leftover.FindString(out); m != "" {
		return "", fmt.Errorf("template marker left: %s", m)
	}
	return out, nil
}

// blocks keeps {{#name}}…{{/name}} sections whose name is active (no nesting).
func blocks(text string, active map[string]bool) (string, error) {
	var b strings.Builder
	for {
		loc := blockOpen.FindStringSubmatchIndex(text)
		if loc == nil {
			b.WriteString(text)
			return b.String(), nil
		}
		name := text[loc[2]:loc[3]]
		closeTag := "{{/" + name + "}}"
		end := strings.Index(text[loc[1]:], closeTag)
		if end < 0 {
			return "", fmt.Errorf("unclosed block {{#%s}}", name)
		}
		b.WriteString(text[:loc[0]])
		if active[name] {
			b.WriteString(text[loc[1] : loc[1]+end])
		}
		text = text[loc[1]+end+len(closeTag):]
	}
}

// Fill resolves @@PLACEHOLDERS@@.
func Fill(text string, values map[string]string) (string, error) {
	var err error
	out := placeholder.ReplaceAllStringFunc(text, func(m string) string {
		key := placeholder.FindStringSubmatch(m)[1]
		v, ok := values[key]
		if !ok {
			err = fmt.Errorf("unresolved placeholder %s", m)
		}
		return v
	})
	return out, err
}

// Rule is one ordered permission rule: action, resource, effect.
type Rule [3]string

// Agent is a neutral agent definition.
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mode        string `json:"mode"`
	Profile     string `json:"profile"`
	Hidden      bool   `json:"hidden"`
	Steps       *int   `json:"steps"`
	Rules       []Rule `json:"rules"`
	Body        string `json:"-"`
}

// LoadAgents reads agents/<name>.json and agents/<name>.md.
func LoadAgents(fsys fs.FS) ([]Agent, error) {
	entries, err := fs.ReadDir(fsys, "agents")
	if err != nil {
		return nil, err
	}
	var agents []Agent
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join("agents", e.Name()))
		if err != nil {
			return nil, err
		}
		var a Agent
		if err := json.Unmarshal(data, &a); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if !slices.Contains(Profiles, a.Profile) {
			return nil, fmt.Errorf("%s: unknown profile %q (one of %s)", e.Name(), a.Profile, strings.Join(Profiles, ", "))
		}
		body, err := fs.ReadFile(fsys, path.Join("agents", strings.TrimSuffix(e.Name(), ".json")+".md"))
		if err != nil {
			return nil, err
		}
		a.Body = string(body)
		agents = append(agents, a)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

// Quote renders a string as a JSON string, which is also a valid YAML scalar.
func Quote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

// Profiles are the agent roles a model is chosen for, in display order:
// planner (orchestrators, specs, plans), analyst (discovery, constitution,
// tasks), executor (implementation), scout (code search, device runs) and
// reviewer (review, verification).
var Profiles = []string{"planner", "analyst", "executor", "scout", "reviewer"}

// Models maps a profile to a model reference.
type Models map[string]string

// Efforts maps a profile to a reasoning effort level; empty keeps the session's.
type Efforts map[string]string

// OpenCodeAgent renders an OpenCode agent file.
func OpenCodeAgent(a Agent, values map[string]string, models Models) (string, error) {
	lines := []string{"---", "description: " + Quote(a.Description)}
	if a.Hidden {
		lines = append(lines, "hidden: true")
	}
	lines = append(lines, "mode: "+a.Mode, "model: "+models[a.Profile], "permissions:")
	for _, r := range a.Rules {
		action, err := Fill(r[0], values)
		if err != nil {
			return "", err
		}
		resource, err := Fill(r[1], values)
		if err != nil {
			return "", err
		}
		lines = append(lines, "- action: "+Quote(action), "  resource: "+Quote(resource), "  effect: "+r[2])
	}
	if a.Steps != nil {
		lines = append(lines, fmt.Sprintf("steps: %d", *a.Steps))
	}
	lines = append(lines, "---")
	body, err := Template(a.Body, "opencode", values)
	if err != nil {
		return "", fmt.Errorf("%s: %w", a.Name, err)
	}
	return strings.Join(lines, "\n") + "\n" + body, nil
}

func globMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$").MatchString(value)
}

func (a Agent) allows(action string) bool {
	for _, r := range a.Rules {
		if r[2] != "deny" && r[0] != "*" && globMatch(r[0], action) {
			return true
		}
	}
	return false
}

func (a Agent) hasNonDeny(action string) bool {
	for _, r := range a.Rules {
		if r[0] == action && r[2] != "deny" {
			return true
		}
	}
	return false
}

// ClaudeTools derives a Claude Code tool list from the agent's permission rules.
func ClaudeTools(a Agent, all []Agent, mcpServers []string) []string {
	var tools []string
	add := func(t ...string) { tools = append(tools, t...) }
	if a.allows("read") {
		add("Read")
	}
	if a.allows("grep") {
		add("Grep")
	}
	if a.allows("glob") {
		add("Glob")
	}
	if a.hasNonDeny("edit") {
		add("Edit", "Write")
	}
	if a.hasNonDeny("shell") {
		add("Bash")
	}
	if a.allows("webfetch") {
		add("WebFetch")
	}
	if a.allows("websearch") {
		add("WebSearch")
	}
	if a.Mode == "primary" && a.allows("question") {
		add("AskUserQuestion")
	}
	if a.allows("todowrite") {
		add("TodoWrite")
	}
	if a.hasNonDeny("skill") {
		add("Skill")
	}
	var subagents []string
	for _, other := range all {
		if other.Name == a.Name {
			continue
		}
		effect := "deny"
		for _, r := range a.Rules {
			if r[0] == "subagent" && globMatch(r[1], other.Name) {
				effect = r[2]
			}
		}
		if effect != "deny" {
			subagents = append(subagents, other.Name)
		}
	}
	if len(subagents) > 0 {
		add("Agent(" + strings.Join(subagents, ", ") + ")")
	}
	// Longest server names first so "foo-bar" wins over "foo".
	servers := append([]string(nil), mcpServers...)
	sort.Slice(servers, func(i, j int) bool { return len(servers[i]) > len(servers[j]) })
	seen := map[string]bool{}
	for _, r := range a.Rules {
		if r[2] == "deny" {
			continue
		}
		for _, s := range servers {
			if strings.HasPrefix(r[0], s+"_") {
				tool := strings.TrimPrefix(r[0], s+"_")
				name := "mcp__" + s + "__" + tool
				if !seen[name] {
					seen[name] = true
					add(name)
				}
				break
			}
		}
	}
	return tools
}

// ClaudeAgent renders a Claude Code subagent file.
func ClaudeAgent(a Agent, all []Agent, values map[string]string, models Models, efforts Efforts, mcpServers []string) (string, error) {
	description := a.Description
	if a.Mode == "primary" {
		description += fmt.Sprintf(" Run it as the main agent with `claude --agent %s`; do not delegate to it.", a.Name)
	}
	lines := []string{"---", "name: " + a.Name, "description: " + Quote(description),
		"tools: " + strings.Join(ClaudeTools(a, all, mcpServers), ", "), "model: " + models[a.Profile]}
	if e := efforts[a.Profile]; e != "" {
		lines = append(lines, "effort: "+e)
	}
	if a.Steps != nil {
		lines = append(lines, fmt.Sprintf("maxTurns: %d", *a.Steps))
	}
	lines = append(lines, "---")
	body, err := Template(a.Body, "claude", values)
	if err != nil {
		return "", fmt.Errorf("%s: %w", a.Name, err)
	}
	return strings.Join(lines, "\n") + "\n" + body, nil
}
