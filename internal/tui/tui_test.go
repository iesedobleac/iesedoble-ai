package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/iesedobleac/iesedoble-ai/internal/installer"
)

func press(t *testing.T, m Model, keys ...tea.KeyMsg) Model {
	t.Helper()
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(Model)
	}
	return m
}

var (
	enter = tea.KeyMsg{Type: tea.KeyEnter}
	down  = tea.KeyMsg{Type: tea.KeyDown}
	esc   = tea.KeyMsg{Type: tea.KeyEsc}
)

func typed(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestModelsScreen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	for i := range m.targets.items {
		m.targets.items[i].checked = true
	}
	m.screen = scPersona
	m = press(t, m, enter)
	if m.screen != scModels || m.modelsReady() {
		t.Fatalf("screen %d, ready %v", m.screen, m.modelsReady())
	}
	m = press(t, m, enter) // ignored while listing
	if m.picking {
		t.Fatal("picker opened before the models were listed")
	}

	next, _ := m.Update(modelsMsg{lists: map[string][]string{"opencode": {"openai/gpt-5.5", "openai/gpt-5.6-terra"}, "claude": installer.ClaudeModels}})
	m = next.(Model)
	opts := m.options()
	if opts.Models["claude"]["planner"] != "opus" || opts.Models["claude"]["scout"] != "haiku" || opts.Efforts["claude"]["planner"] != "high" {
		t.Errorf("Claude defaults not preselected: %v %v", opts.Models["claude"], opts.Efforts["claude"])
	}
	if v := opts.Models["opencode"]["planner"]; v != "" {
		t.Errorf("unlisted OpenCode default preselected: %q", v)
	}
	if _, ok := opts.Efforts["opencode"]; ok {
		t.Error("OpenCode rows should have no effort")
	}

	// Continue is refused while a row has no model.
	m.modelFocus = len(m.visibleModels())
	m = press(t, m, enter)
	if m.screen != scModels || m.modelNote == "" {
		t.Fatalf("continued without models: screen %d", m.screen)
	}

	// OpenCode rows, in profile order: planner, analyst, executor, scout, reviewer.
	m.modelFocus = 0
	if view := m.View(); !strings.Contains(view, "A bad plan compounds") {
		t.Errorf("planner guide missing:\n%s", view)
	}
	m = press(t, m, enter, typed("terra"))
	if got := m.pickOptions(); len(got) != 1 {
		t.Fatalf("filtered options: %v", got)
	}
	m = press(t, m, enter)                                                       // planner: filtered
	m = press(t, m, down, enter, down, down, enter, typed("other/model"), enter) // analyst: Custom…
	for range 3 {                                                                // executor, scout, reviewer: first listed
		m = press(t, m, down, enter, enter)
	}
	if m.picking {
		t.Fatal("picker still open")
	}
	got := m.options().Models["opencode"]
	if got["planner"] != "openai/gpt-5.6-terra" || got["analyst"] != "other/model" || got["reviewer"] != "openai/gpt-5.5" {
		t.Errorf("OpenCode models: %v", got)
	}

	// Claude rows carry an effort that ←/→ cycles; the guide mentions it.
	m = press(t, m, down) // Claude Code planner
	if view := m.View(); !strings.Contains(view, "effort high") || !strings.Contains(view, "←/→ changes the effort") {
		t.Errorf("effort not shown:\n%s", view)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyRight})
	if e := m.options().Efforts["claude"]["planner"]; e != "xhigh" {
		t.Errorf("effort after →: %q", e)
	}

	m.modelFocus = len(m.visibleModels())
	m = press(t, m, enter)
	if m.screen != scSettings {
		t.Errorf("screen %d after Continue", m.screen)
	}
}

func TestPickerWithoutList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	for i := range m.targets.items {
		m.targets.items[i].checked = m.targets.items[i].key == "opencode"
	}
	m.screen = scPersona
	m = press(t, m, enter)
	next, _ := m.Update(modelsMsg{lists: map[string][]string{"opencode": nil}, errs: map[string]string{"opencode": "opencode models: timed out after 60s"}})
	m = next.(Model)
	m = press(t, m, enter)
	if !m.picking || !m.pickCustom {
		t.Fatal("expected the model ID input when nothing is listed")
	}
	if view := m.View(); !strings.Contains(view, "timed out after 60s") {
		t.Errorf("reason not shown:\n%s", view)
	}
	m = press(t, m, typed("p/m"), enter)
	if v := m.options().Models["opencode"]["planner"]; v != "p/m" {
		t.Errorf("planner %q", v)
	}
}

func TestReviewView(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	for i := range m.targets.items {
		m.targets.items[i].checked = true
	}
	for i := range m.components.items {
		m.components.items[i].checked = m.components.items[i].key != "codegraph"
	}
	for i, k := range m.modelKeys {
		m.values[i] = installer.DefaultModels[k[0]][k[1]]
	}
	view := m.reviewView()
	for _, want := range []string{"Assistants", "Components", "Models", "– CodeGraph", "planner", "github-copilot/claude-opus-5.5"} {
		if !strings.Contains(view, want) {
			t.Errorf("review view missing %q:\n%s", want, view)
		}
	}
}

func TestProgressSteps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	m.screen, m.logCh = scRunning, make(chan logLine, 1)
	next, _ := m.Update(logLine{kind: "step", text: "OpenCode: rules", done: 3, total: 14})
	m = next.(Model)
	if m.step.done != 3 || len(m.logs) != 0 {
		t.Fatalf("step %+v, logs %v", m.step, m.logs)
	}
	if view := m.View(); !strings.Contains(view, "3/14") || !strings.Contains(view, "OpenCode: rules") {
		t.Errorf("running view:\n%s", view)
	}
}

func TestHeaderFitsWidth(t *testing.T) {
	if h := header(100); !strings.Contains(h, "▀█▀ █▀▀▀▀") || !strings.Contains(h, "agents, skills") {
		t.Errorf("wide header:\n%s", h)
	}
	if h := header(40); strings.Contains(h, "███") || !strings.Contains(h, "iesedoble-ai") {
		t.Errorf("narrow header:\n%s", h)
	}
}

func TestMenuView(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saved := installer.Version
	defer func() { installer.Version = saved }()
	installer.Version = "0.4.2"

	m := New()
	view := m.View()
	for _, want := range []string{"github.com/iesedobleac/iesedoble-ai", "Nothing installed yet", "Menu", "› Install or update", "- Sync"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu missing %q:\n%s", want, view)
		}
	}

	m.status = &installer.InstallStatus{Targets: []string{"opencode", "claude"}, Installed: time.Now().Add(-50 * time.Hour), Version: "0.4.1"}
	next, _ := m.Update(latestMsg("0.4.3"))
	view = next.(Model).View()
	for _, want := range []string{"✓ OpenCode · Claude Code", "installed 2 days ago", "Installed with v0.4.1", "Sync to update it to v0.4.2", "v0.4.3 available"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu missing %q:\n%s", want, view)
		}
	}

	// Up to date: no sync or upgrade notice.
	m.status.Version, m.latest = "0.4.2", "0.4.2"
	if view := m.View(); strings.Contains(view, "Installed with") || strings.Contains(view, "available") {
		t.Errorf("notices shown while up to date:\n%s", view)
	}
}

func TestPersonaChoice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	if opts := m.options(); opts.Components.Persona != "architect" || !opts.Components.Concise {
		t.Fatalf("defaults: %+v", opts.Components)
	}
	for i, it := range m.persona.items {
		if it.key == "persona-none" {
			m.persona.cursor = i
		}
	}
	m.persona.update(" ")
	m.persona.update(" ") // selecting a choice twice keeps it selected
	opts := m.options()
	if opts.Components.Persona != "none" {
		t.Errorf("persona after choosing None: %q", opts.Components.Persona)
	}
	selected := 0
	for _, it := range m.persona.items {
		if it.group == "persona" && it.checked {
			selected++
		}
	}
	if selected != 1 {
		t.Errorf("%d personas selected", selected)
	}
	m.persona.update("a") // toggling all leaves the persona choice alone
	if m.options().Components.Persona != "none" {
		t.Error("toggle-all changed the persona")
	}
	if view := m.persona.view(); !strings.Contains(view, "(•) None") || !strings.Contains(view, "Persona") || !strings.Contains(view, "Style") {
		t.Errorf("components view:\n%s", view)
	}
}

func TestUpgradeEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	savedVersion, savedUpgrade := installer.Version, selfUpgrade
	defer func() { installer.Version, selfUpgrade = savedVersion, savedUpgrade }()
	installer.Version = "0.4.2"

	selfUpgrade = func() []string { return nil }
	next, _ := New().Update(latestMsg("0.4.3"))
	m := next.(Model)
	if view := m.View(); strings.Contains(view, "Upgrade to") || !strings.Contains(view, "v0.4.3 available") {
		t.Errorf("without a self-upgrade the menu shows only the command:\n%s", view)
	}

	selfUpgrade = func() []string { return []string{"true"} }
	m = New()
	m = press(t, m, down) // on Sync before the release is known
	next, _ = m.Update(latestMsg("0.4.3"))
	m = next.(Model)
	view := m.View()
	if !strings.Contains(view, "Upgrade to v0.4.3") || strings.Contains(view, "v0.4.3 available") {
		t.Errorf("menu with an upgrade:\n%s", view)
	}
	if m.menuEntries()[m.menu].act != actSync {
		t.Errorf("cursor moved off Sync to %q", m.menuEntries()[m.menu].label)
	}

	next, _ = m.Update(upgradedMsg{})
	if m := next.(Model); !m.upgraded {
		t.Error("a successful upgrade should hand over to the new binary")
	}
	next, _ = m.Update(upgradedMsg{err: errors.New("exit status 1")})
	if m := next.(Model); m.upgraded || m.screen != scDone || m.err == nil {
		t.Errorf("a failed upgrade should show the error: %+v", m.err)
	}
}

func TestSpecLanguageChoice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	if l := m.options().Components.SpecLanguage; l != "es" {
		t.Fatalf("default spec language %q", l)
	}
	for i, it := range m.persona.items {
		if it.key == "spec-en" {
			m.persona.cursor = i
		}
	}
	m.persona.update(" ")
	opts := m.options()
	if opts.Components.SpecLanguage != "en" || opts.Components.Persona != "architect" {
		t.Errorf("after choosing English: %+v", opts.Components)
	}
	if view := m.persona.view(); !strings.Contains(view, "SDD documents") || !strings.Contains(view, "(•) English") {
		t.Errorf("persona view:\n%s", view)
	}
}

func TestPersonaStep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	m.screen = scComponents
	m = press(t, m, enter)
	if m.screen != scPersona {
		t.Fatalf("components should lead to the persona step, got %d", m.screen)
	}
	view := m.View()
	for _, want := range []string{"(3/6)", "Architect", "verifies before agreeing and corrects you with evidence", "~300 tokens"} {
		if !strings.Contains(view, want) {
			t.Errorf("persona step missing %q:\n%s", want, view)
		}
	}
	m = press(t, m, down, down) // onto Concise: its explanation replaces the architect's
	if view := m.View(); !strings.Contains(view, "Before:") || strings.Contains(view, "corrects you with evidence") {
		t.Errorf("details for Concise:\n%s", view)
	}
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != scComponents {
		t.Errorf("esc should go back to components, got %d", m.screen)
	}
	for _, it := range m.components.items {
		if it.group != "" || it.key == "concise" {
			t.Errorf("components still list %q", it.key)
		}
	}
}

func TestSettingsScreen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New()
	for i := range m.targets.items {
		m.targets.items[i].checked = true
	}
	m.screen = scSettings

	// Claude Code first, then OpenCode: sandbox, auto-compact, subagent monitor.
	view := m.View()
	for _, want := range []string{"(5/6)", "Claude Code", "Sandbox", "Auto-compact", "OpenCode", "Subagent monitor", "/sandbox"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings view lacks %q:\n%s", want, view)
		}
	}
	m = press(t, m, typed(" "))                                                           // sandbox on
	m = press(t, m, down, tea.KeyMsg{Type: tea.KeyRight}, tea.KeyMsg{Type: tea.KeyRight}) // 150k
	m = press(t, m, down, typed(" "))                                                     // monitor on
	opts := m.options()
	if !opts.Components.ClaudeSandbox || opts.ClaudeAutoCompact != 150_000 || !opts.Components.SubagentMonitor {
		t.Errorf("settings: sandbox %v, window %d, monitor %v",
			opts.Components.ClaudeSandbox, opts.ClaudeAutoCompact, opts.Components.SubagentMonitor)
	}
	if !strings.Contains(m.reviewView(), "at 150k tokens") {
		t.Errorf("review lacks the settings:\n%s", m.reviewView())
	}

	// Only the selected assistants' settings show.
	for i := range m.targets.items {
		m.targets.items[i].checked = m.targets.items[i].key == "opencode"
	}
	if rows := m.settingRows(); len(rows) != 1 || rows[0].key != "monitor" {
		t.Errorf("OpenCode-only rows: %v", rows)
	}

	m = press(t, m, enter)
	if m.screen != scReview {
		t.Errorf("screen %d after enter", m.screen)
	}
	m = press(t, m, esc)
	if m.screen != scSettings {
		t.Errorf("esc from review: screen %d", m.screen)
	}
}
