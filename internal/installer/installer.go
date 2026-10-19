// Package installer installs, updates and removes the ecosystem for each assistant.
package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/iesedobleac/iesedoble-ai/assets"
	"github.com/iesedobleac/iesedoble-ai/internal/render"
)

// Version is set at build time.
var Version = "dev"

// toolName names the state folder, the managed blocks and the logs.
const toolName = "iesedoble-ai"

const (
	markStart   = "<!-- " + toolName + ":start -->"
	markEnd     = "<!-- " + toolName + ":end -->"
	profileMark = "# " + toolName
	profileLine = "export ENGRAM_NUDGE_COOLDOWN_SECS=86400"
)

// Components the user can select.
type Components struct {
	SDD       bool `json:"sdd"`       // sdd-* agents and the sdd-workflow skill
	Android   bool `json:"android"`   // android-orchestrator (its subagents also come with SDD)
	Skills    bool `json:"skills"`    // general skills (skill-generator)
	Plugin    bool `json:"plugin"`    // OpenCode context-warning plugin
	Engram    bool `json:"engram"`    // memory MCP and plugin
	CodeGraph bool `json:"codegraph"` // code graph MCP
	// SubagentMonitor keeps the OpenCode subagent statusline that `engram setup`
	// adds to tui.json; off removes it.
	SubagentMonitor bool `json:"subagentMonitor"`
	// ClaudeSandbox turns on Claude Code's sandbox for shell commands, with
	// the exceptions the agents need.
	ClaudeSandbox bool `json:"claudeSandbox"`
	// Persona is the voice added once to the global rules: one of Personas.
	// Installs recorded before it existed have it empty and keep "architect",
	// the persona the orchestrators used to carry.
	Persona string `json:"persona,omitempty"`
	// Concise adds the caveman-style concise reply rules to the global rules.
	Concise bool `json:"concise"`
}

// Personas are the persona choices; "none" adds nothing.
var Personas = []string{"none", "architect"}

// DefaultComponents selects everything but the subagent monitor, with the
// architect persona and the concise style.
func DefaultComponents() Components {
	return Components{SDD: true, Android: true, Skills: true, Plugin: true, Engram: true, CodeGraph: true,
		Persona: "architect", Concise: true}
}

// SelectedPersona is the persona to install, "architect" for installs that
// predate the choice.
func (c Components) SelectedPersona() string {
	if c.Persona == "" {
		return "architect"
	}
	return c.Persona
}

// Options configure a run.
type Options struct {
	Targets []string                 `json:"targets"`
	Models  map[string]render.Models `json:"models"`
	// Efforts per target and profile; missing ones take DefaultEfforts.
	Efforts    map[string]render.Efforts `json:"efforts,omitempty"`
	Components Components                `json:"components"`
	// ClaudeAutoCompact is the Claude Code auto-compact window in tokens; 0
	// keeps Claude Code's own default.
	ClaudeAutoCompact int    `json:"claudeAutoCompact,omitempty"`
	ConfigDir         string `json:"configDir,omitempty"`
	ClaudeDir         string `json:"claudeDir,omitempty"`
	DeliverablesDir   string `json:"deliverablesDir,omitempty"`
	AndroidSDK        string `json:"androidSdk,omitempty"`
	SkipDeps          bool   `json:"-"`
	NoShellProfile    bool   `json:"noShellProfile"`
	NoGitBash         bool   `json:"-"`
	DryRun            bool   `json:"-"`
	Home              string `json:"-"` // for tests; defaults to the user's home
}

// Logger receives progress messages.
type Logger interface {
	Info(msg string)
	OK(msg string)
	Warn(msg string)
}

// Progress is implemented by loggers that show how far an install has got.
// Step is called as each step starts, with the number of steps already done.
type Progress interface {
	Step(done, total int, label string)
}

// Result summarizes an install.
type Result struct {
	Targets   []string
	NextSteps []string
	BackupDir string
}

// TargetState records what was installed for one assistant.
type TargetState struct {
	Dir   string            `json:"dir"`
	Files map[string]string `json:"files"`
	MCP   []string          `json:"mcp,omitempty"`
	Deny  []string          `json:"deny,omitempty"`
	// AutoCompact is the auto-compact window this tool wrote, so it can be
	// removed later without touching one set by the user.
	AutoCompact int `json:"autoCompact,omitempty"`
	// Sandbox is what this tool added to Claude Code's sandbox settings.
	Sandbox   *SandboxState `json:"sandbox,omitempty"`
	Version   string        `json:"version"`
	Installed string        `json:"installed"`
}

// SandboxState records the sandbox settings this tool added, so they can be
// removed without touching the user's own.
type SandboxState struct {
	Enabled  bool     `json:"enabled,omitempty"` // this tool set sandbox.enabled
	Excluded []string `json:"excluded,omitempty"`
	Write    []string `json:"write,omitempty"`
}

// State is ~/.iesedoble-ai/state.json.
type State struct {
	Targets   map[string]*TargetState `json:"targets"`
	Profile   string                  `json:"profile,omitempty"`
	Selection *Options                `json:"selection,omitempty"`
}

type run struct {
	opts   Options
	log    Logger
	home   string
	stamp  string
	bins   map[string]string
	backup string
	// Files already backed up, or created by this run, are not backed up again.
	seen map[string]bool
	// Install steps done and expected, for loggers that show progress.
	steps, total int
}

// step reports that the next install step starts.
func (r *run) step(label string) {
	if p, ok := r.log.(Progress); ok {
		p.Step(min(r.steps, r.total), r.total, label)
	}
	r.steps++
}

func newRun(opts Options, log Logger) (*run, error) {
	home := opts.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = h
	}
	if opts.DeliverablesDir == "" {
		opts.DeliverablesDir = filepath.Join(home, "agent-deliverables")
	}
	opts.Models = withDefaultModels(opts.Models)
	opts.Efforts = withDefaultEfforts(opts.Efforts)
	stamp := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	r := &run{opts: opts, log: log, home: home, stamp: stamp, bins: map[string]string{}, seen: map[string]bool{}}
	r.backup = filepath.Join(r.stateDir(), "backups", stamp)
	return r, nil
}

// withDefaultModels fills the models not chosen with each target's defaults.
func withDefaultModels(chosen map[string]render.Models) map[string]render.Models {
	out := map[string]render.Models{}
	for t, defaults := range DefaultModels {
		m := render.Models{}
		for k, v := range defaults {
			m[k] = v
		}
		for k, v := range chosen[t] {
			if v != "" {
				m[k] = v
			}
		}
		out[t] = m
	}
	return out
}

// withDefaultEfforts fills the efforts not chosen with each target's defaults.
func withDefaultEfforts(chosen map[string]render.Efforts) map[string]render.Efforts {
	out := map[string]render.Efforts{}
	for t, defaults := range DefaultEfforts {
		e := render.Efforts{}
		for k, v := range defaults {
			e[k] = v
		}
		for k, v := range chosen[t] {
			if v != "" {
				e[k] = v
			}
		}
		out[t] = e
	}
	return out
}

// efforts is what goes into a target's agent files: "auto" keeps the session's.
func (r *run) efforts(target string) render.Efforts {
	out := render.Efforts{}
	for k, v := range r.opts.Efforts[target] {
		if v != "auto" {
			out[k] = v
		}
	}
	return out
}

// ── Paths ────────────────────────────────────────────────────────────────────

func (r *run) stateDir() string  { return stateDirFor(r.home) }
func (r *run) stateFile() string { return filepath.Join(r.stateDir(), "state.json") }

func expandHome(p, home string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(home, p[1:])
	}
	return p
}

func slash(p string) string { return filepath.ToSlash(p) }

func which(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return slash(p)
}

func command(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (r *run) androidSDK() string {
	if r.opts.AndroidSDK != "" {
		return expandHome(r.opts.AndroidSDK, r.home)
	}
	for _, v := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if d := os.Getenv(v); d != "" {
			return d
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(r.home, "Library/Android/sdk")
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(r.home, "AppData/Local")
		}
		return filepath.Join(local, "Android/Sdk")
	}
	return filepath.Join(r.home, "Android/Sdk")
}

// values fill the placeholders of the files rendered for a target.
func (r *run) values(t Target) map[string]string {
	gradle := os.Getenv("GRADLE_USER_HOME")
	if gradle == "" {
		gradle = filepath.Join(r.home, ".gradle")
	}
	dirs := t.dirs(r)
	bin := func(name string) string {
		// Bare names survive Node version switches (nvm); Windows needs the .cmd/.exe path.
		if runtime.GOOS == "windows" && r.bins[name] != "" {
			return r.bins[name]
		}
		return name
	}
	deliverables, _ := filepath.Abs(expandHome(r.opts.DeliverablesDir, r.home))
	return map[string]string{
		"OPENCODE_TMP":         slash(dirs.tmp),
		"OPENCODE_TOOL_OUTPUT": slash(filepath.Join(dirs.data, "tool-output")),
		"DELIVERABLES":         slash(deliverables),
		"ANDROID_SDK":          slash(r.androidSDK()),
		"GRADLE_CACHES":        slash(filepath.Join(gradle, "caches")),
		"SKILLS_DIR":           slash(dirs.skills),
		"BIN_ENGRAM":           bin("engram"),
		"BIN_CODEGRAPH":        bin("codegraph"),
	}
}

// ── Files, backups and blocks ────────────────────────────────────────────────

func sum(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

func (r *run) backupFile(file string) {
	abs, _ := filepath.Abs(file)
	if r.opts.DryRun || r.seen[abs] {
		return
	}
	r.seen[abs] = true
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}
	rel := strings.TrimPrefix(slash(abs), "/")
	rel = strings.Replace(rel, ":", "", 1)
	dest := filepath.Join(r.backup, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(dest), 0o755)
	_ = os.WriteFile(dest, data, 0o644)
}

func (r *run) write(file, text string) error {
	if r.opts.DryRun {
		r.log.Info("[dry-run] write " + file)
		return nil
	}
	if abs, _ := filepath.Abs(file); !r.seen[abs] {
		// Not backed up yet: the file is new, so later writes need no backup either.
		if _, err := os.Stat(file); os.IsNotExist(err) {
			r.seen[abs] = true
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(text), 0o644)
}

func (r *run) remove(file string) {
	if _, err := os.Stat(file); err != nil {
		return
	}
	r.backupFile(file)
	if r.opts.DryRun {
		r.log.Info("[dry-run] remove " + file)
		return
	}
	_ = os.Remove(file)
}

// syncFiles writes rendered files under root, backing up files the user changed,
// and removes files a previous run installed that are no longer rendered.
func (r *run) syncFiles(root string, files map[string]string, previous map[string]string) (map[string]string, error) {
	installed := map[string]string{}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, rel := range keys {
		text := files[rel]
		dest := filepath.Join(root, filepath.FromSlash(rel))
		if current, err := os.ReadFile(dest); err == nil {
			if string(current) == text {
				installed[rel] = sum(text)
				continue
			}
			if previous[rel] != sum(string(current)) {
				r.backupFile(dest)
			}
		}
		if err := r.write(dest, text); err != nil {
			return nil, err
		}
		installed[rel] = sum(text)
	}
	for rel := range previous {
		if _, ok := installed[rel]; !ok {
			r.remove(filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	return installed, nil
}

var (
	// Blocks written under the legacy name are matched too, and replaced.
	blockRe = regexp.MustCompile(`(?s)\n*<!-- ` + names + `:start -->.*?<!-- ` + names + `:end -->\n?`)
	innerRe = regexp.MustCompile(`(?s)<!-- ` + names + `:start -->.*?<!-- ` + names + `:end -->`)
)

func (r *run) upsertBlock(file, body string) error {
	block := markStart + "\n" + strings.TrimSpace(body) + "\n" + markEnd
	currentBytes, _ := os.ReadFile(file)
	current := string(currentBytes)
	var next string
	if innerRe.MatchString(current) {
		next = innerRe.ReplaceAllLiteralString(current, block)
	} else if strings.TrimSpace(current) != "" {
		next = strings.TrimRight(current, "\n\t ") + "\n\n" + block + "\n"
	} else {
		next = block + "\n"
	}
	if next == current {
		return nil
	}
	r.backupFile(file)
	return r.write(file, next)
}

func (r *run) removeBlock(file string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	rest := strings.TrimSpace(blockRe.ReplaceAllLiteralString(string(data), "\n"))
	r.backupFile(file)
	if rest != "" {
		return r.write(file, rest+"\n")
	}
	if !r.opts.DryRun {
		return os.Remove(file)
	}
	return nil
}

func pruneEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			pruneEmpty(filepath.Join(dir, e.Name()))
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

// ── JSON / JSONC ─────────────────────────────────────────────────────────────

func stripJSONC(text string) string {
	var b strings.Builder
	inString := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case inString:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(text) {
				i++
				b.WriteByte(text[i])
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			i += 2
			for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			i++
		default:
			b.WriteByte(c)
		}
	}
	return regexp.MustCompile(`,(\s*[}\]])`).ReplaceAllString(b.String(), "$1")
}

func hasComments(text string) bool {
	noStrings := regexp.MustCompile(`"(?:\\.|[^"\\])*"`).ReplaceAllString(text, `""`)
	return strings.Contains(noStrings, "//") || strings.Contains(noStrings, "/*")
}

func readJSON(file string) (map[string]any, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if strings.TrimSpace(string(data)) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(stripJSONC(string(data))), &out); err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", file, err)
	}
	return out, nil
}

func (r *run) writeJSON(file string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return r.write(file, string(data)+"\n")
}

func object(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	v := map[string]any{}
	m[key] = v
	return v
}

// ── State ────────────────────────────────────────────────────────────────────

func (r *run) readState() *State {
	s := &State{Targets: map[string]*TargetState{}}
	if data, err := os.ReadFile(r.stateFile()); err == nil {
		_ = json.Unmarshal(data, s)
	}
	if s.Targets == nil {
		s.Targets = map[string]*TargetState{}
	}
	return s
}

func (r *run) writeState(s *State) error { return r.writeJSON(r.stateFile(), s) }

// ReadSelection returns the options saved by the last install, for sync.
func ReadSelection(home string) (*Options, error) {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	data, err := os.ReadFile(filepath.Join(stateDirFor(home), "state.json"))
	if err != nil {
		return nil, fmt.Errorf("no previous install found (%w)", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Selection == nil {
		return nil, errors.New("state.json has no saved selection")
	}
	return s.Selection, nil
}

// ── Sources ──────────────────────────────────────────────────────────────────

type sources struct {
	agents []render.Agent
}

func (r *run) loadSources() (*sources, error) {
	agents, err := render.LoadAgents(assets.FS)
	if err != nil {
		return nil, err
	}
	src := &sources{}
	c := r.opts.Components
	keep := func(name string) bool {
		switch {
		case strings.HasPrefix(name, "sdd-"):
			return c.SDD
		case name == "android-orchestrator":
			return c.Android
		case strings.HasPrefix(name, "android-"):
			// Subagents shared by both orchestrators.
			return c.Android || c.SDD
		}
		return true
	}
	for _, a := range agents {
		if keep(a.Name) {
			src.agents = append(src.agents, a)
		}
	}
	return src, nil
}

func (r *run) skillFiles(target string, values map[string]string) (map[string]string, error) {
	files := map[string]string{}
	add := func(fsys fs.FS, name string) error {
		return fs.WalkDir(fsys, name, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || path.Base(p) == ".DS_Store" {
				return err
			}
			data, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			text, err := render.Template(string(data), target, values)
			if err != nil {
				return fmt.Errorf("skill %s: %w", p, err)
			}
			files["skills/"+p] = text
			return nil
		})
	}
	skillsFS, _ := fs.Sub(assets.FS, "skills")
	if r.opts.Components.SDD {
		if err := add(skillsFS, "sdd-workflow"); err != nil {
			return nil, err
		}
	}
	if r.opts.Components.Skills {
		if err := add(skillsFS, "skill-generator"); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// rules is the managed block of AGENTS.md / CLAUDE.md: the shared rules, then
// the persona and the concise style when selected, so each is loaded once.
func (r *run) rules(target string, values map[string]string) (string, error) {
	files := []string{"rules/rules.md"}
	if p := r.opts.Components.SelectedPersona(); p != "none" {
		files = append(files, "persona/"+p+".md")
	}
	if r.opts.Components.Concise {
		files = append(files, "persona/concise.md")
	}
	var parts []string
	for _, f := range files {
		data, err := fs.ReadFile(assets.FS, f)
		if err != nil {
			return "", err
		}
		text, err := render.Template(string(data), target, values)
		if err != nil {
			return "", err
		}
		parts = append(parts, strings.TrimSpace(text))
	}
	return strings.Join(parts, "\n\n"), nil
}

func (r *run) servers(values map[string]string) (map[string]render.MCPServer, []string, error) {
	data, err := fs.ReadFile(assets.FS, "mcp.json")
	if err != nil {
		return nil, nil, err
	}
	text, err := render.Fill(string(data), values)
	if err != nil {
		return nil, nil, err
	}
	catalog, err := render.ParseCatalog([]byte(text))
	if err != nil {
		return nil, nil, err
	}
	wanted := map[string]render.MCPServer{}
	c := r.opts.Components
	for name, s := range catalog["core"] {
		if (name == "engram" && c.Engram) || (name == "codegraph" && c.CodeGraph) {
			wanted[name] = s
		}
	}
	return wanted, append(render.ServerNames(catalog["core"]), userServers...), nil
}

// userServers are MCP servers the user configures outside the installer. They
// are never installed; they are listed only so agent rules name their tools.
var userServers = []string{"context7", "firebase"}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
