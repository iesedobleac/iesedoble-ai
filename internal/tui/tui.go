// Package tui is the interactive installer.
package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/iesedobleac/iesedoble-ai/internal/installer"
	"github.com/iesedobleac/iesedoble-ai/internal/render"
)

var (
	accent   = lipgloss.Color("#7AA2F7")
	okColor  = lipgloss.Color("#9ECE6A")
	warnCol  = lipgloss.Color("#E0AF68")
	dim      = lipgloss.Color("#737AA2")
	title    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtle   = lipgloss.NewStyle().Foreground(dim)
	selected = lipgloss.NewStyle().Foreground(accent).Bold(true)
	okStyle  = lipgloss.NewStyle().Foreground(okColor)
	warnSty  = lipgloss.NewStyle().Foreground(warnCol)
	box      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dim).Padding(1, 2)
)

type screen int

const (
	scMenu screen = iota
	scTargets
	scComponents
	scPersona
	scModels
	scSettings
	scReview
	scChecking
	scRequirements
	scRunning
	scDone
)

type item struct {
	key, label, desc string
	checked          bool
	// group makes the item one choice of several: selecting it clears the
	// others with the same group, and it shows as ( ) / (•).
	group string
	// section is a heading shown above the item when it differs from the
	// previous item's.
	section string
}

type checklist struct {
	items  []item
	cursor int
}

func (c *checklist) update(key string) {
	switch key {
	case "up", "k":
		if c.cursor > 0 {
			c.cursor--
		}
	case "down", "j":
		if c.cursor < len(c.items)-1 {
			c.cursor++
		}
	case " ", "x":
		it := &c.items[c.cursor]
		if it.group == "" {
			it.checked = !it.checked
			break
		}
		for i := range c.items {
			if c.items[i].group == it.group {
				c.items[i].checked = false
			}
		}
		it.checked = true
	case "a": // toggles the independent items; choices keep their selection
		all := true
		for _, it := range c.items {
			all = all && (it.checked || it.group != "")
		}
		for i := range c.items {
			if c.items[i].group == "" {
				c.items[i].checked = !all
			}
		}
	}
}

func (c checklist) view() string {
	var b strings.Builder
	for i, it := range c.items {
		if it.section != "" && (i == 0 || c.items[i-1].section != it.section) {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(subtle.Render(it.section) + "\n")
		}
		mark := "[ ]"
		switch {
		case it.group != "" && it.checked:
			mark = "(•)"
		case it.group != "":
			mark = "( )"
		case it.checked:
			mark = "[x]"
		}
		var line string
		switch {
		case i == c.cursor:
			line = selected.Render("› " + mark + " " + it.label)
		case it.checked:
			line = "  " + okStyle.Render(mark) + " " + it.label
		default:
			line = "  " + mark + " " + it.label
		}
		b.WriteString(line)
		if it.desc != "" {
			b.WriteString(subtle.Render("  " + it.desc))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (c checklist) checked() []string {
	var out []string
	for _, it := range c.items {
		if it.checked {
			out = append(out, it.key)
		}
	}
	return out
}

type action int

const (
	actInstall action = iota
	actSync
	actDoctor
	actUninstall
	actQuit
)

var menuItems = []struct {
	act         action
	label, desc string
}{
	{actInstall, "Install or update", "choose assistants, components and models"},
	{actSync, "Sync", "re-apply the last selection (after an upgrade)"},
	{actDoctor, "Doctor", "read-only health check"},
	{actUninstall, "Uninstall", "remove everything this tool installed"},
	{actQuit, "Quit", ""},
}

// logLine is a log message, or with kind "step" an install step starting.
type logLine struct {
	kind, text  string
	done, total int
}

type reqsMsg []installer.Requirement

type doneMsg struct {
	res *installer.Result
	ok  bool
	err error
}

// modelsMsg carries the listed models per target; a target that could not be
// listed has a nil list and the reason in errs.
type modelsMsg struct {
	lists map[string][]string
	errs  map[string]string
}

// pickRows is how many models the picker shows at once.
const pickRows = 10

// Model is the Bubble Tea model.
type Model struct {
	screen     screen
	menu       int
	act        action
	detected   []string
	targets    checklist
	components checklist
	persona    checklist   // persona choice and concise style
	values     []string    // chosen model per modelKeys entry; empty until chosen
	efforts    []string    // effort per modelKeys entry; empty when the target has none
	modelKeys  [][2]string // target, profile
	modelFocus int         // row on the models screen; the last one is Continue
	settingRow int         // row on the settings screen
	compact    int         // Claude Code auto-compact window; 0 keeps its default
	sandbox    bool        // Claude Code sandbox
	monitor    bool        // OpenCode subagent monitor
	modelNote  string
	avail      map[string][]string // listed models per target; nil when they could not be listed
	availErr   map[string]string   // why a target's models could not be listed
	listed     map[string]bool     // targets whose models were looked up
	picking    bool
	pickFilter textinput.Model
	pickCursor int
	pickCustom bool
	pickInput  textinput.Model
	review     int
	dryRun     bool
	pending    action            // what the requirements screen confirms
	pendingOpt installer.Options // options it runs with
	reqs       []installer.Requirement
	reqButton  int
	spin       spinner.Model
	bar        progress.Model
	step       logLine // last install step reported
	logs       []logLine
	logCh      chan logLine
	res        *installer.Result
	err        error
	ok         bool
	width      int
	status     *installer.InstallStatus // recorded install, for the menu
	latest     string                   // latest released version, once checked
}

// New builds the model, preselecting the previous selection when there is one.
func New() Model {
	prev, _ := installer.ReadSelection("")
	detected := installer.DetectTargets(installer.Options{})
	m := Model{detected: detected, status: installer.ReadStatus("")}

	selectedTargets := detected
	comps := installer.DefaultComponents()
	if prev != nil {
		selectedTargets, comps, m.compact = prev.Targets, prev.Components, prev.ClaudeAutoCompact
	}
	for _, t := range installer.AllTargets {
		desc := ""
		if containsStr(detected, t) {
			desc = "detected"
		}
		m.targets.items = append(m.targets.items, item{key: t, label: installer.TargetLabels[t], desc: desc, checked: containsStr(selectedTargets, t)})
	}
	m.components.items = []item{
		{key: "sdd", label: "SDD flow", desc: "sdd-orchestrator, spec/plan/tasks/apply/verify agents, sdd-workflow skill", checked: comps.SDD},
		{key: "android", label: "Android orchestrator", desc: "android-orchestrator (its explore/worker/review/device subagents come with either)", checked: comps.Android},
		{key: "skills", label: "General skills", desc: "skill-generator", checked: comps.Skills},
		{key: "plugin", label: "Context warning", desc: "OpenCode: past 200k tokens, offers to compact or start a new session", checked: comps.Plugin},
		{key: "engram", label: "Engram memory", desc: "persistent memory MCP and plugin", checked: comps.Engram},
		{key: "codegraph", label: "CodeGraph", desc: "code graph MCP", checked: comps.CodeGraph},
	}
	m.sandbox, m.monitor = comps.ClaudeSandbox, comps.SubagentMonitor
	m.persona.items = []item{
		{key: "persona-architect", label: "Architect",
			desc:    "senior architect: verifies before agreeing, challenges with evidence, one question at a time (~300 tokens)",
			checked: comps.SelectedPersona() == "architect", group: "persona", section: "Persona"},
		{key: "persona-none", label: "None", desc: "your assistant's default voice; adds no tokens",
			checked: comps.SelectedPersona() == "none", group: "persona", section: "Persona"},
		{key: "concise", label: "Concise",
			desc:    "caveman-style replies: answer first, no filler, code and errors verbatim; cuts output tokens (~600 tokens)",
			checked: comps.Concise, section: "Style"},
	}

	models, efforts := map[string]render.Models{}, map[string]render.Efforts{}
	if prev != nil && prev.Models != nil {
		models = prev.Models
	}
	if prev != nil && prev.Efforts != nil {
		efforts = prev.Efforts
	}
	// Model defaults are filled in once the models are listed, and only if available.
	for _, t := range installer.AllTargets {
		for _, profile := range render.Profiles {
			effort := ""
			if defaults, ok := installer.DefaultEfforts[t]; ok {
				effort = defaults[profile]
				if e := efforts[t][profile]; e != "" {
					effort = e
				}
			}
			m.values = append(m.values, models[t][profile])
			m.efforts = append(m.efforts, effort)
			m.modelKeys = append(m.modelKeys, [2]string{t, profile})
		}
	}
	m.avail, m.availErr, m.listed = map[string][]string{}, map[string]string{}, map[string]bool{}
	m.pickFilter = textinput.New()
	m.pickFilter.Prompt = "Filter: "
	m.pickInput = textinput.New()
	m.pickInput.Prompt = "Model ID: "
	m.pickInput.CharLimit = 120
	m.pickInput.Width = 50
	m.spin = spinner.New()
	m.spin.Spinner = spinner.Dot
	m.bar = newBar()
	return m
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return checkLatest }

// latestMsg carries the latest released version.
type latestMsg string

// checkLatest asks GitHub for the latest release, giving up after a few
// seconds so the menu never waits on the network.
func checkLatest() tea.Msg {
	if _, err := strconv.Atoi(strings.SplitN(installer.Version, ".", 2)[0]); err != nil {
		return nil // development build
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, err := installer.LatestVersion(ctx)
	if err != nil {
		return nil
	}
	return latestMsg(v)
}

func (m Model) options() installer.Options {
	opts := installer.Options{Targets: m.targets.checked(), DryRun: m.dryRun,
		Models: map[string]render.Models{}, ClaudeAutoCompact: m.compact}
	comps := map[string]bool{}
	for _, k := range append(m.components.checked(), m.persona.checked()...) {
		comps[k] = true
	}
	opts.Components = installer.Components{SDD: comps["sdd"], Android: comps["android"], Skills: comps["skills"],
		Plugin: comps["plugin"], Engram: comps["engram"], CodeGraph: comps["codegraph"],
		SubagentMonitor: m.monitor, ClaudeSandbox: m.sandbox, Persona: "none", Concise: comps["concise"]}
	if comps["persona-architect"] {
		opts.Components.Persona = "architect"
	}
	opts.Efforts = map[string]render.Efforts{}
	for i, k := range m.modelKeys {
		if m.efforts[i] != "" {
			if opts.Efforts[k[0]] == nil {
				opts.Efforts[k[0]] = render.Efforts{}
			}
			opts.Efforts[k[0]][k[1]] = m.efforts[i]
		}
		if opts.Models[k[0]] == nil {
			opts.Models[k[0]] = render.Models{}
		}
		opts.Models[k[0]][k[1]] = m.values[i]
	}
	return opts
}

// modelsReady reports whether the models of every selected target were looked up.
func (m Model) modelsReady() bool {
	for _, t := range m.targets.checked() {
		if !m.listed[t] {
			return false
		}
	}
	return true
}

// enterModels opens the models screen, listing the models it has not looked up yet.
func (m Model) enterModels() (Model, tea.Cmd) {
	m.screen, m.modelFocus, m.modelNote, m.picking = scModels, 0, "", false
	var todo []string
	for _, t := range m.targets.checked() {
		if !m.listed[t] {
			todo = append(todo, t)
		}
	}
	if len(todo) == 0 {
		return m, nil
	}
	return m, tea.Batch(m.spin.Tick, func() tea.Msg {
		out := modelsMsg{lists: map[string][]string{}, errs: map[string]string{}}
		for _, t := range todo {
			list, err := installer.AvailableModels(t)
			out.lists[t] = list
			if err != nil {
				out.lists[t] = nil
				out.errs[t] = err.Error()
			}
		}
		return out
	})
}

// pickOptions are the listed models of the target being picked that match the filter.
func (m Model) pickOptions() []string {
	t := m.modelKeys[m.visibleModels()[m.modelFocus]][0]
	filter := strings.ToLower(strings.TrimSpace(m.pickFilter.Value()))
	var out []string
	for _, v := range m.avail[t] {
		if strings.Contains(strings.ToLower(v), filter) {
			out = append(out, v)
		}
	}
	return out
}

// openPicker starts choosing the model of the focused row.
func (m Model) openPicker() (Model, tea.Cmd) {
	i := m.visibleModels()[m.modelFocus]
	m.picking, m.pickCursor, m.modelNote = true, 0, ""
	m.pickFilter.SetValue("")
	m.pickInput.SetValue(m.values[i])
	m.pickCustom = m.avail[m.modelKeys[i][0]] == nil
	if m.pickCustom {
		m.pickFilter.Blur()
		return m, m.pickInput.Focus()
	}
	for n, v := range m.pickOptions() {
		if v == m.values[i] {
			m.pickCursor = n
		}
	}
	m.pickInput.Blur()
	return m, m.pickFilter.Focus()
}

func (m Model) handlePicker(msg tea.KeyMsg, key string) (Model, tea.Cmd) {
	i := m.visibleModels()[m.modelFocus]
	var cmd tea.Cmd
	if m.pickCustom {
		switch key {
		case "enter":
			if v := strings.TrimSpace(m.pickInput.Value()); v != "" {
				m.values[i], m.picking = v, false
			}
		case "esc":
			if m.avail[m.modelKeys[i][0]] == nil {
				m.picking = false
			} else {
				m.pickCustom = false
				m.pickInput.Blur()
				cmd = m.pickFilter.Focus()
			}
		default:
			m.pickInput, cmd = m.pickInput.Update(msg)
		}
		return m, cmd
	}
	opts := m.pickOptions()
	switch key {
	case "up":
		if m.pickCursor > 0 {
			m.pickCursor--
		}
	case "down":
		if m.pickCursor < len(opts) { // the last row is Custom…
			m.pickCursor++
		}
	case "enter":
		if m.pickCursor < len(opts) {
			m.values[i], m.picking = opts[m.pickCursor], false
			return m, nil
		}
		m.pickCustom = true
		if f := strings.TrimSpace(m.pickFilter.Value()); f != "" {
			m.pickInput.SetValue(f)
		}
		m.pickFilter.Blur()
		return m, m.pickInput.Focus()
	case "esc":
		m.picking = false
	default:
		m.pickFilter, cmd = m.pickFilter.Update(msg)
		m.pickCursor = 0
	}
	return m, cmd
}

// wrap breaks text into lines of at most width runes, at spaces.
func wrap(text string, width int) string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(text) {
		if line != "" && len([]rune(line))+1+len([]rune(w)) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	return strings.Join(append(lines, line), "\n")
}

func (m Model) pickerView() string {
	var b strings.Builder
	i := m.visibleModels()[m.modelFocus]
	k := m.modelKeys[i]
	b.WriteString(title.Render(fmt.Sprintf("%s %s model", installer.TargetLabels[k[0]], k[1])) + "\n\n")
	if m.pickCustom {
		if m.avail[k[0]] == nil {
			b.WriteString(warnSty.Render("Could not list the "+installer.TargetLabels[k[0]]+" models. Type a model ID, or fix the cause and run again:") + "\n")
			b.WriteString(subtle.Render(wrap(m.availErr[k[0]], max(40, m.width-4))) + "\n\n")
		}
		b.WriteString(m.pickInput.View() + "\n")
		return b.String() + help("type a model ID · enter confirm · esc back")
	}
	b.WriteString(m.pickFilter.View() + "\n\n")
	opts := append(m.pickOptions(), "Custom…")
	start := 0
	if m.pickCursor >= pickRows {
		start = m.pickCursor - pickRows + 1
	}
	for n := start; n < len(opts) && n < start+pickRows; n++ {
		label := opts[n]
		switch {
		case n == m.pickCursor:
			b.WriteString(selected.Render("› "+label) + "\n")
		case label == m.values[i]:
			b.WriteString("  " + okStyle.Render(label) + "\n")
		default:
			b.WriteString("  " + label + "\n")
		}
	}
	if len(opts) > pickRows {
		b.WriteString(subtle.Render(fmt.Sprintf("  %d models, type to filter", len(opts)-1)) + "\n")
	}
	return b.String() + help("↑/↓ move · type to filter · enter choose · esc back")
}

// visibleModels are the model inputs for the selected targets.
func (m Model) visibleModels() []int {
	var idx []int
	sel := m.targets.checked()
	for i, k := range m.modelKeys {
		if containsStr(sel, k[0]) {
			idx = append(idx, i)
		}
	}
	return idx
}

type chanLogger chan logLine

func (c chanLogger) Info(s string) { c <- logLine{kind: "info", text: s} }
func (c chanLogger) OK(s string)   { c <- logLine{kind: "ok", text: s} }
func (c chanLogger) Warn(s string) { c <- logLine{kind: "warn", text: s} }

// Step implements installer.Progress.
func (c chanLogger) Step(done, total int, label string) {
	c <- logLine{kind: "step", text: label, done: done, total: total}
}

func newBar() progress.Model {
	return progress.New(progress.WithGradient(string(accent), string(okColor)), progress.WithWidth(40))
}

func waitLog(ch chan logLine) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return line
	}
}

func (m Model) start(act action, opts installer.Options) (Model, tea.Cmd) {
	m.screen, m.act, m.logs, m.step, m.bar = scRunning, act, nil, logLine{}, newBar()
	m.logCh = make(chan logLine, 256)
	ch := m.logCh
	run := func() tea.Msg {
		log := chanLogger(ch)
		var msg doneMsg
		switch act {
		case actInstall:
			msg.res, msg.err = installer.Install(opts, log)
			msg.ok = msg.err == nil
		case actSync:
			prev, err := installer.ReadSelection("")
			if err != nil {
				msg.err = err
				break
			}
			msg.res, msg.err = installer.Install(*prev, log)
			msg.ok = msg.err == nil
		case actDoctor:
			msg.ok = installer.Doctor(installer.Options{}, log)
		case actUninstall:
			msg.err = installer.Uninstall(installer.Options{}, log)
			msg.ok = msg.err == nil
		}
		close(ch)
		return msg
	}
	return m, tea.Batch(m.spin.Tick, run, waitLog(ch))
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.bar.Width = max(20, min(60, msg.Width-16))
		return m, nil
	case logLine:
		if msg.kind == "step" {
			m.step = msg
			return m, tea.Batch(m.bar.SetPercent(float64(msg.done)/float64(max(msg.total, 1))), waitLog(m.logCh))
		}
		m.logs = append(m.logs, msg)
		return m, waitLog(m.logCh)
	case progress.FrameMsg:
		bar, cmd := m.bar.Update(msg)
		m.bar = bar.(progress.Model)
		return m, cmd
	case reqsMsg:
		m.screen, m.reqs, m.reqButton = scRequirements, msg, 0
		if m.blocked() {
			m.reqButton = 1
		}
		return m, nil
	case doneMsg:
		m.screen, m.res, m.err, m.ok = scDone, msg.res, msg.err, msg.ok
		m.status = installer.ReadStatus("")
		return m, nil
	case latestMsg:
		m.latest = string(msg)
		return m, nil
	case modelsMsg:
		for t, list := range msg.lists {
			m.avail[t], m.availErr[t], m.listed[t] = list, msg.errs[t], true
		}
		// Preselect a default only where the assistant lists it; otherwise the user chooses.
		for i, k := range m.modelKeys {
			if _, ok := msg.lists[k[0]]; ok && m.values[i] == "" {
				if def := installer.DefaultModels[k[0]][k[1]]; installer.KnownModel(k[0], def, m.avail[k[0]]) {
					m.values[i] = def
				}
			}
		}
		return m, nil
	case spinner.TickMsg:
		if m.screen == scRunning || m.screen == scChecking || (m.screen == scModels && !m.modelsReady()) {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleKey(msg, key)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	switch m.screen {
	case scMenu:
		switch key {
		case "up", "k":
			if m.menu > 0 {
				m.menu--
			}
		case "down", "j":
			if m.menu < len(menuItems)-1 {
				m.menu++
			}
		case "q", "esc":
			return m, tea.Quit
		case "enter":
			switch menuItems[m.menu].act {
			case actInstall:
				m.screen = scTargets
			case actSync:
				prev, err := installer.ReadSelection("")
				if err != nil {
					m.screen, m.err, m.ok, m.res, m.logs = scDone, err, false, nil, nil
					return m, nil
				}
				return m.check(actSync, *prev)
			case actDoctor:
				return m.start(actDoctor, installer.Options{})
			case actUninstall:
				return m.start(actUninstall, installer.Options{})
			case actQuit:
				return m, tea.Quit
			}
		}
	case scTargets, scComponents, scPersona:
		list := &m.targets
		next, prev := scComponents, scMenu
		switch m.screen {
		case scComponents:
			list, next, prev = &m.components, scPersona, scTargets
		case scPersona:
			list, next, prev = &m.persona, scModels, scComponents
		}
		switch key {
		case "enter":
			if m.screen == scTargets && len(m.targets.checked()) == 0 {
				return m, nil
			}
			if next == scModels {
				return m.enterModels()
			}
			m.screen = next
		case "esc":
			m.screen = prev
		default:
			list.update(key)
		}
	case scModels:
		if m.picking {
			return m.handlePicker(msg, key)
		}
		vis := m.visibleModels()
		switch key {
		case "esc":
			m.screen = scPersona
		case "up", "k", "shift+tab":
			if m.modelFocus > 0 {
				m.modelFocus--
			}
		case "down", "j", "tab":
			if m.modelFocus < len(vis) {
				m.modelFocus++
			}
		case "left", "right", "h", "l":
			if m.modelFocus < len(vis) && m.efforts[vis[m.modelFocus]] != "" {
				m.efforts[vis[m.modelFocus]] = cycle(installer.EffortLevels, m.efforts[vis[m.modelFocus]], key == "right" || key == "l")
			}
		case "enter", " ":
			if !m.modelsReady() {
				return m, nil
			}
			if m.modelFocus < len(vis) {
				return m.openPicker()
			}
			for _, i := range vis {
				if m.values[i] == "" {
					m.modelNote = "Choose a model for every row."
					return m, nil
				}
			}
			m.screen, m.settingRow = scSettings, 0
		}
	case scSettings:
		rows := m.settingRows()
		m.settingRow = min(m.settingRow, max(len(rows)-1, 0))
		switch key {
		case "esc":
			m.screen, m.modelFocus, m.modelNote = scModels, 0, ""
		case "enter":
			m.screen, m.review = scReview, 0
		case "up", "k", "shift+tab":
			if m.settingRow > 0 {
				m.settingRow--
			}
		case "down", "j", "tab":
			if m.settingRow < len(rows)-1 {
				m.settingRow++
			}
		case "left", "right", "h", "l", " ", "x":
			if len(rows) > 0 {
				m = m.changeSetting(rows[m.settingRow], key != "left" && key != "h")
			}
		}
	case scReview:
		switch key {
		case "left", "h", "up", "k":
			if m.review > 0 {
				m.review--
			}
		case "right", "l", "down", "j", "tab":
			if m.review < 2 {
				m.review++
			}
		case "esc":
			m.screen = scSettings
		case "enter":
			switch m.review {
			case 0:
				m.dryRun = false
				return m.check(actInstall, m.options())
			case 1:
				m.dryRun = true
				return m.check(actInstall, m.options())
			case 2:
				m.screen = scMenu
			}
		}
	case scRequirements:
		switch key {
		case "left", "h", "up", "k":
			if m.reqButton > 0 && !m.blocked() {
				m.reqButton--
			}
		case "right", "l", "down", "j", "tab":
			if m.reqButton < 1 {
				m.reqButton++
			}
		case "esc":
			m.screen = m.back()
		case "enter":
			if m.reqButton == 0 {
				return m.start(m.pending, m.pendingOpt)
			}
			m.screen = m.back()
		}
	case scDone:
		switch key {
		case "enter", "esc":
			m.screen = scMenu
		case "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func help(keys string) string { return "\n" + subtle.Render(keys) }

// View implements tea.Model.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(header(m.width) + "\n\n")
	switch m.screen {
	case scMenu:
		b.WriteString(m.menuView())
		b.WriteString("\n  " + subtle.Render("↑/↓ move · enter select · q quit"))
	case scTargets:
		b.WriteString(title.Render("Assistants") + subtle.Render("  (1/6)") + "\n\n" + m.targets.view())
		b.WriteString(help("space toggle · a all · enter next · esc back"))
	case scComponents:
		b.WriteString(title.Render("Components") + subtle.Render("  (2/6)") + "\n\n" + m.components.view())
		b.WriteString(help("space toggle · a all · enter next · esc back"))
	case scPersona:
		b.WriteString(title.Render("Persona") + subtle.Render("  (3/6) how your assistant talks to you") + "\n\n" + m.persona.view())
		if d := personaDetails[m.persona.items[m.persona.cursor].key]; len(d) > 0 {
			b.WriteString("\n" + box.Render(strings.Join(d, "\n")) + "\n")
		}
		b.WriteString(help("↑/↓ move · space choose · enter next · esc back"))
	case scModels:
		b.WriteString(title.Render("Models") + subtle.Render("  (4/6) one model per role; the panel explains what each role needs") + "\n\n")
		switch {
		case !m.modelsReady():
			b.WriteString(m.spin.View() + " Listing the available models…\n")
		case m.picking:
			b.WriteString(m.pickerView())
		default:
			vis := m.visibleModels()
			valueWidth := len("choose a model")
			for _, i := range vis {
				valueWidth = max(valueWidth, len(m.values[i]))
			}
			for n, i := range vis {
				k := m.modelKeys[i]
				label := fmt.Sprintf("%-12s %-9s", installer.TargetLabels[k[0]], k[1])
				value := fmt.Sprintf("%-*s", valueWidth, m.values[i])
				if m.values[i] == "" {
					value = warnSty.Render(fmt.Sprintf("%-*s", valueWidth, "choose a model"))
				}
				if m.efforts[i] != "" {
					value += subtle.Render("  effort ") + m.efforts[i]
				}
				if n == m.modelFocus {
					b.WriteString(selected.Render("› "+label) + " " + value + "\n")
				} else {
					b.WriteString("  " + label + " " + value + "\n")
				}
			}
			cont := "Continue →"
			if m.modelFocus == len(vis) {
				cont = selected.Render("› " + cont)
			} else {
				cont = "  " + cont
			}
			b.WriteString("\n" + cont + "\n")
			if m.modelNote != "" {
				b.WriteString("\n" + warnSty.Render(m.modelNote) + "\n")
			}
			if m.modelFocus < len(vis) {
				k := m.modelKeys[vis[m.modelFocus]]
				guide := profileGuide[k[1]]
				if m.efforts[vis[m.modelFocus]] != "" {
					guide = append(append([]string{}, guide...), "", "←/→ changes the effort; auto keeps your session's level.")
				}
				b.WriteString("\n" + box.Render(strings.Join(guide, "\n")) + "\n")
			}
			b.WriteString(help("↑/↓ move · enter choose model · ←/→ effort · esc back"))
		}
	case scSettings:
		b.WriteString(title.Render("Settings") + subtle.Render("  (5/6) how each assistant behaves") + "\n\n" + m.settingsView())
		b.WriteString(help("↑/↓ move · space toggle · ←/→ change · enter next · esc back"))
	case scReview:
		b.WriteString(title.Render("Review") + subtle.Render("  (6/6)") + "\n\n" + box.Render(m.reviewView()) + "\n\n")
		buttons := []string{"Install", "Dry run", "Cancel"}
		for i, btn := range buttons {
			if i == m.review {
				b.WriteString(selected.Render("[ "+btn+" ]") + "  ")
			} else {
				b.WriteString("  " + btn + "    ")
			}
		}
		b.WriteString("\n" + help("←/→ choose · enter confirm · esc back"))
	case scChecking:
		b.WriteString(m.spin.View() + " Checking the tools this selection needs…\n")
	case scRequirements:
		b.WriteString(title.Render("Required tools") + subtle.Render("  checked before anything is written") + "\n\n")
		b.WriteString(requirementsView(m.reqs) + "\n")
		blocked := m.blocked()
		switch {
		case blocked:
			b.WriteString(warnSty.Render("Install the tools marked ✗ and run again.") + "\n\n")
		case m.dryRun || m.pendingOpt.DryRun:
			b.WriteString(subtle.Render("Dry run: nothing is written.") + "\n\n")
		}
		label := "Install"
		if m.pending == actSync {
			label = "Sync"
		}
		if m.pendingOpt.DryRun {
			label = "Dry run"
		}
		buttons := []string{label, "Back"}
		for i, btn := range buttons {
			switch {
			case i == 0 && blocked:
				b.WriteString(subtle.Render("  " + btn + "    "))
			case i == m.reqButton:
				b.WriteString(selected.Render("[ "+btn+" ]") + "  ")
			default:
				b.WriteString("  " + btn + "    ")
			}
		}
		b.WriteString("\n" + help("←/→ choose · enter confirm · esc back"))
	case scRunning:
		if m.step.total == 0 {
			b.WriteString(m.spin.View() + " Working…\n\n" + m.logView(14))
			break
		}
		b.WriteString(title.Render("Installing") + "\n\n")
		b.WriteString(m.bar.View() + subtle.Render(fmt.Sprintf("  %d/%d", m.step.done, m.step.total)) + "\n")
		b.WriteString(m.spin.View() + " " + m.step.text + "\n\n" + m.logView(12))
	case scDone:
		switch {
		case m.err != nil:
			b.WriteString(warnSty.Render("✗ "+m.err.Error()) + "\n\n")
		case m.ok:
			b.WriteString(okStyle.Render("✓ Done") + "\n\n")
		default:
			b.WriteString(warnSty.Render("! Finished with warnings") + "\n\n")
		}
		b.WriteString(m.logView(18))
		if m.res != nil && len(m.res.NextSteps) > 0 {
			b.WriteString("\n" + title.Render("Next steps") + "\n")
			for i, s := range m.res.NextSteps {
				b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, s))
			}
		}
		if m.res != nil && m.res.BackupDir != "" {
			b.WriteString(subtle.Render("\nBackups: "+m.res.BackupDir) + "\n")
		}
		b.WriteString(help("enter menu · q quit"))
	}
	return indentLines(b.String(), margin) + "\n"
}

// margin is the blank space kept on the left of every screen.
const margin = 2

// indentLines puts n spaces before every non-empty line.
func indentLines(text string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = pad + l
		}
	}
	return strings.Join(lines, "\n")
}

// profileGuide explains what each model role does and what suits it; the
// choice stays the user's.
var profileGuide = map[string][]string{
	"planner": {
		"Planner: orchestrators, sdd-spec, sdd-clarify, sdd-plan.",
		"Decides what to build and how. A bad plan compounds into a bad",
		"implementation, so this is where quality pays most.",
		"",
		"Suits: your strongest model, high effort.",
	},
	"analyst": {
		"Analyst: sdd-discovery, sdd-constitution, sdd-tasks, android-research.",
		"Understands the project, researches documentation, and structures the work.",
		"",
		"Suits: a balanced model, medium effort.",
	},
	"executor": {
		"Executor: android-worker, sdd-apply.",
		"Implements closed, already-designed tasks: the thinking happened in the plan.",
		"",
		"Suits: a balanced model, low effort. The cheapest model only for trivial edits.",
	},
	"scout": {
		"Scout: android-explore, android-device.",
		"Searches code and runs device checks; speed matters more than depth.",
		"",
		"Suits: your fastest, cheapest model, low effort.",
	},
	"reviewer": {
		"Reviewer: android-review, sdd-verify.",
		"Checks changes against the task. A model of a different family than the",
		"executor catches different mistakes than the one that wrote the code.",
		"",
		"Suits: a strong model, ideally another family; medium effort.",
	},
}

// compactLabel names an auto-compact window.
func compactLabel(tokens int) string {
	if tokens == 0 {
		return "Claude Code's default"
	}
	return fmt.Sprintf("at %dk tokens", tokens/1000)
}

// setting is one row of the settings screen.
type setting struct {
	target, key, label string
	guide              []string // explains the row under the cursor
}

// settings per assistant, shown when the assistant is selected.
var settings = []setting{
	{"claude", "sandbox", "Sandbox", []string{
		"Claude Code's sandbox for shell commands: they write only inside the",
		"project and have no network, so they run without permission prompts.",
		"",
		"adb and ./gradlew run outside it and ask as usual; the deliverables",
		"folder stays writable. Needs macOS, Linux (bubblewrap and socat) or",
		"WSL2; on native Windows it is not turned on. Check it with /sandbox.",
	}},
	{"claude", "autocompact", "Auto-compact", []string{
		"When Claude Code summarizes the conversation on its own and carries",
		"on in the same session.",
		"",
		"A smaller window keeps sessions cheaper and focused; a larger one keeps",
		"more history. Claude Code's default compacts near the model's limit.",
		"",
		"Saved as autoCompactWindow in Claude Code's settings.json;",
		"/autocompact still overrides it per model.",
	}},
	{"opencode", "monitor", "Subagent monitor", []string{
		"The subagent statusline that Engram's setup adds to OpenCode.",
		"",
		"Off removes it from tui.json and cli.json, after a backup.",
	}},
}

// settingRows are the settings of the selected assistants, in order.
func (m Model) settingRows() []setting {
	var rows []setting
	for _, s := range settings {
		if containsStr(m.targets.checked(), s.target) {
			rows = append(rows, s)
		}
	}
	return rows
}

// settingValue shows a setting's current value.
func (m Model) settingValue(s setting) string {
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	switch s.key {
	case "sandbox":
		return onOff(m.sandbox)
	case "monitor":
		return onOff(m.monitor)
	default:
		return compactLabel(m.compact)
	}
}

// changeSetting toggles a switch or moves a value forward or back.
func (m Model) changeSetting(s setting, forward bool) Model {
	switch s.key {
	case "sandbox":
		m.sandbox = !m.sandbox
	case "monitor":
		m.monitor = !m.monitor
	default:
		m.compact = cycle(installer.AutoCompactWindows, m.compact, forward)
	}
	return m
}

// settingsView lists the settings grouped by assistant, with the guide of
// the row under the cursor.
func (m Model) settingsView() string {
	var b strings.Builder
	rows := m.settingRows()
	if len(rows) == 0 {
		return subtle.Render("Nothing to set for the selected assistants.") + "\n"
	}
	m.settingRow = min(m.settingRow, len(rows)-1)
	for i, s := range rows {
		if i == 0 || rows[i-1].target != s.target {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(subtle.Render(installer.TargetLabels[s.target]) + "\n")
		}
		label := fmt.Sprintf("%-17s", s.label)
		if i == m.settingRow {
			b.WriteString(selected.Render("› "+label) + " " + m.settingValue(s) + "\n")
		} else {
			b.WriteString("  " + label + " " + m.settingValue(s) + "\n")
		}
	}
	b.WriteString("\n" + box.Render(strings.Join(rows[m.settingRow].guide, "\n")) + "\n")
	return b.String()
}

// cycle returns the value after (or before) current in values, wrapping around.
func cycle[T comparable](values []T, current T, forward bool) T {
	i := slices.Index(values, current)
	if forward {
		return values[(i+1)%len(values)]
	}
	if i <= 0 {
		return values[len(values)-1]
	}
	return values[i-1]
}

// personaDetails explain the option under the cursor on the persona step.
var personaDetails = map[string][]string{
	"persona-architect": {
		"Talks like a senior architect who cares that you understand:",
		"• verifies before agreeing and corrects you with evidence",
		"• one question at a time; alternatives only at a real fork",
		"• pushes back on code without context; teaches in depth only when you ask",
		"",
		"Cost: ~300 tokens, loaded once from AGENTS.md / CLAUDE.md.",
	},
	"persona-none": {
		"Keeps your assistant's own voice: nothing is added to AGENTS.md / CLAUDE.md.",
		"",
		"Cost: no tokens.",
	},
	"concise": {
		"Caveman-style replies, adapted from caveman (Apache-2.0):",
		"• answer first; no greetings, hedging or recaps",
		"• code, commands, paths and errors stay verbatim",
		"• full sentences for warnings, irreversible actions and questions",
		"",
		"Before: \"Sure! I'd be happy to help. The issue is likely caused by…\"",
		"After:  \"Bug in auth middleware. Expiry check uses `<` not `<=`. Fix:\"",
		"",
		"Cost: ~600 tokens of rules; it cuts output tokens, the expensive ones.",
	},
}

// menuView is the links, the install status and the menu options.
func (m Model) menuView() string {
	var b strings.Builder
	b.WriteString("  " + link(installer.RepoURL, "github.com/iesedobleac/iesedoble-ai") +
		subtle.Render(" · ") + link(installer.GuideURL, "install and usage guide") + "\n\n")

	st := m.status
	if st == nil {
		b.WriteString("  " + subtle.Render("• Nothing installed yet: choose Install or update to start") + "\n")
	} else {
		var names []string
		for _, id := range st.Targets {
			name := installer.TargetLabels[id]
			if name == "" {
				name = id
			}
			names = append(names, name)
		}
		line := "  " + okStyle.Render("✓ ") + strings.Join(names, " · ")
		if !st.Installed.IsZero() {
			line += subtle.Render("  installed " + ago(time.Since(st.Installed)))
		}
		b.WriteString(line + "\n")
		if installer.NewerVersion(st.Version, installer.Version) {
			b.WriteString("  " + warnSty.Render("! Installed with v"+st.Version+": choose Sync to update it to v"+installer.Version) + "\n")
		}
	}
	if installer.NewerVersion(installer.Version, m.latest) {
		b.WriteString("  " + selected.Render("↑ v"+m.latest+" available: ") + installer.UpgradeCommand() + "\n")
	}

	b.WriteString("\n  " + title.Render("Menu") + "\n\n")
	width := 0
	for _, it := range menuItems {
		width = max(width, len(it.label))
	}
	for i, it := range menuItems {
		label := fmt.Sprintf("%-*s", width, it.label)
		if i == m.menu {
			b.WriteString("  " + selected.Render("› "+label))
		} else {
			b.WriteString("  " + subtle.Render("- ") + label)
		}
		b.WriteString(subtle.Render("   "+it.desc) + "\n")
	}
	return b.String()
}

// link is text that terminals with hyperlink support (OSC 8) open on click.
func link(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + subtle.Render(text) + "\x1b]8;;\x1b\\"
}

// ago is a short, human duration: "an hour ago", "yesterday", "3 days ago".
func ago(d time.Duration) string {
	switch days := int(d.Hours() / 24); {
	case d < time.Hour:
		return "just now"
	case d < 2*time.Hour:
		return "an hour ago"
	case days == 0:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case days == 1:
		return "yesterday"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

// check computes the requirements for a pending install or sync.
func (m Model) check(act action, opts installer.Options) (Model, tea.Cmd) {
	if len(opts.Targets) == 0 {
		opts.Targets = installer.DetectTargets(opts)
	}
	m.screen, m.pending, m.pendingOpt = scChecking, act, opts
	return m, tea.Batch(m.spin.Tick, func() tea.Msg { return reqsMsg(installer.Requirements(opts)) })
}

func (m Model) blocked() bool {
	return !m.pendingOpt.DryRun && len(installer.Blockers(m.reqs)) > 0
}

// back returns to the screen before the requirements check.
func (m Model) back() screen {
	if m.pending == actSync {
		return scMenu
	}
	return scReview
}

func requirementsView(reqs []installer.Requirement) string {
	var b strings.Builder
	width := 0
	for _, r := range reqs {
		width = max(width, len(r.Name))
	}
	for _, r := range reqs {
		name := fmt.Sprintf("%-*s", width, r.Name)
		switch r.Status {
		case installer.Present:
			b.WriteString(okStyle.Render("✓ ") + name + "  " + subtle.Render(r.Detail) + "\n")
		case installer.WillInstall:
			b.WriteString(selected.Render("↓ ") + name + "  will be installed " + r.Detail + "\n")
		case installer.Optional:
			b.WriteString(warnSty.Render("! ") + name + "  missing: " + r.Hint + "\n")
		case installer.Blocking:
			b.WriteString(warnSty.Bold(true).Render("✗ "+name) + "  required: " + r.Hint + "\n")
		}
		b.WriteString(subtle.Render(fmt.Sprintf("  %-*s  %s", width, "", r.Purpose)) + "\n")
	}
	return b.String()
}

func (m Model) logView(max int) string {
	logs := m.logs
	if len(logs) > max {
		logs = logs[len(logs)-max:]
	}
	var b strings.Builder
	for _, l := range logs {
		switch l.kind {
		case "ok":
			b.WriteString(okStyle.Render("✓ ") + l.text + "\n")
		case "warn":
			b.WriteString(warnSty.Render("! ") + l.text + "\n")
		default:
			b.WriteString(subtle.Render("• ") + l.text + "\n")
		}
	}
	return b.String()
}

// reviewView summarizes the selection in sections: assistants, components and models.
func (m Model) reviewView() string {
	opts := m.options()
	section := lipgloss.NewStyle().Bold(true)
	var b strings.Builder

	b.WriteString(section.Render("Assistants") + "\n")
	for _, t := range opts.Targets {
		b.WriteString("  " + okStyle.Render("✓") + " " + installer.TargetLabels[t] + "\n")
	}

	// Components in two columns: chosen ones ticked, the rest dimmed.
	b.WriteString("\n" + section.Render("Components") + "\n")
	var parts []item // persona and style get their own line below
	for _, it := range m.components.items {
		if it.section == "" {
			parts = append(parts, it)
		}
	}
	width := 0
	for _, it := range parts {
		width = max(width, len(it.label))
	}
	half := (len(parts) + 1) / 2
	for r := 0; r < half; r++ {
		line := "  " + componentCell(parts[r], width)
		if r+half < len(parts) {
			line += "    " + componentCell(parts[r+half], width)
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	persona, style := "None", "normal"
	for _, it := range m.persona.items {
		switch {
		case it.group == "persona" && it.checked:
			persona = it.label
		case it.key == "concise" && it.checked:
			style = "concise"
		}
	}
	b.WriteString("\n" + section.Render("Persona") + "\n  " + persona + subtle.Render(" · ") + style + " replies\n")
	if rows := m.settingRows(); len(rows) > 0 {
		b.WriteString("\n" + section.Render("Settings") + "\n")
		for _, s := range rows {
			b.WriteString("  " + subtle.Render(fmt.Sprintf("%-12s ", installer.TargetLabels[s.target])) +
				s.label + subtle.Render(" · ") + m.settingValue(s) + "\n")
		}
	}

	// Models as a table: one row per profile, one column per assistant; the
	// effort follows the model where the assistant has one.
	cell := func(t, p string) string {
		if e := opts.Efforts[t][p]; e != "" {
			return opts.Models[t][p] + " · " + e
		}
		return opts.Models[t][p]
	}
	widths := map[string]int{}
	for _, t := range opts.Targets {
		widths[t] = len(installer.TargetLabels[t])
		for _, p := range render.Profiles {
			widths[t] = max(widths[t], len(cell(t, p)))
		}
	}
	b.WriteString("\n" + section.Render("Models") + "\n")
	head := fmt.Sprintf("  %-9s", "")
	for _, t := range opts.Targets {
		head += fmt.Sprintf("  %-*s", widths[t], installer.TargetLabels[t])
	}
	b.WriteString(subtle.Render(strings.TrimRight(head, " ")) + "\n")
	for _, p := range render.Profiles {
		row := "  " + subtle.Render(fmt.Sprintf("%-9s", p))
		for _, t := range opts.Targets {
			row += fmt.Sprintf("  %-*s", widths[t], cell(t, p))
		}
		b.WriteString(strings.TrimRight(row, " ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func componentCell(it item, width int) string {
	if it.checked {
		return okStyle.Render("✓") + " " + fmt.Sprintf("%-*s", width, it.label)
	}
	return subtle.Render("– " + fmt.Sprintf("%-*s", width, it.label))
}

// Run starts the interactive installer.
func Run() error {
	_, err := tea.NewProgram(New(), tea.WithAltScreen()).Run()
	return err
}
