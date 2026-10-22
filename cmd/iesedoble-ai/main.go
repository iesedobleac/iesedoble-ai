// Command iesedoble-ai installs agents, skills, rules and MCP servers for AI assistants.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/iesedobleac/iesedoble-ai/internal/installer"
	"github.com/iesedobleac/iesedoble-ai/internal/render"
	"github.com/iesedobleac/iesedoble-ai/internal/tui"
)

var version = "dev"

const usage = `iesedoble-ai %s

Usage:
  iesedoble-ai                    interactive installer
  iesedoble-ai install [flags]    install or update without the menu
  iesedoble-ai sync [--dry-run]   re-apply the last selection (after an upgrade)
  iesedoble-ai uninstall [--targets ...] [--dry-run]
  iesedoble-ai doctor             read-only health check
  iesedoble-ai version

Run "iesedoble-ai install --help" for the install flags.
`

type cliLogger struct{}

func (cliLogger) Info(s string) { fmt.Println("• " + s) }
func (cliLogger) OK(s string)   { fmt.Println("✓ " + s) }
func (cliLogger) Warn(s string) { fmt.Fprintln(os.Stderr, "! "+s) }

type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			*l = append(*l, s)
		}
	}
	return nil
}

func main() {
	installer.Version = version
	if len(os.Args) < 2 {
		if term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd())) {
			if err := tui.Run(); err != nil {
				fail(err)
			}
			return
		}
		fmt.Printf(usage, version)
		return
	}
	switch os.Args[1] {
	case "install":
		install(os.Args[2:])
	case "sync":
		syncCmd(os.Args[2:])
	case "uninstall":
		uninstall(os.Args[2:])
	case "doctor":
		if !installer.Doctor(installer.Options{}, cliLogger{}) {
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Println("iesedoble-ai " + version)
	case "help", "--help", "-h":
		fmt.Printf(usage, version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		fmt.Fprintf(os.Stderr, usage, version)
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "✗ "+err.Error())
	os.Exit(1)
}

func install(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	var targets, without, with listFlag
	fs.Var(&targets, "targets", "assistants: "+strings.Join(installer.AllTargets, ", ")+" (default: detected)")
	fs.Var(&without, "without", "skip components: sdd, android, skills, plugin, engram, codegraph")
	fs.Var(&with, "with", "add components that are off by default: subagent-monitor, claude-sandbox")
	persona := fs.String("persona", "architect", "persona added to the global rules: "+strings.Join(installer.Personas, ", "))
	specLanguage := fs.String("spec-language", "es", "language of the SDD spec, plan and tasks: "+strings.Join(installer.SpecLanguages, ", "))
	concise := fs.Bool("concise", true, "caveman-style concise replies (use --concise=false to skip)")
	models, efforts := map[string]render.Models{}, map[string]render.Efforts{}
	for _, t := range installer.AllTargets {
		models[t] = render.Models{}
		for _, p := range render.Profiles {
			name := installer.ModelFlagPrefix(t) + p
			target, profile := t, p
			fs.Func(name, fmt.Sprintf("%s %s model (default %s)", installer.TargetLabels[t], p, installer.DefaultModels[t][p]),
				func(v string) error { models[target][profile] = v; return nil })
			if defaults, ok := installer.DefaultEfforts[t]; ok {
				fs.Func(name+"-effort", fmt.Sprintf("%s %s effort: %s (default %s)", installer.TargetLabels[t], p,
					strings.Join(installer.EffortLevels, ", "), defaults[p]),
					func(v string) error {
						if efforts[target] == nil {
							efforts[target] = render.Efforts{}
						}
						efforts[target][profile] = v
						return nil
					})
			}
		}
	}
	autoCompact := fs.Int("claude-autocompact", 0, fmt.Sprintf("Claude Code auto-compact window in tokens, %d to %d (default: Claude Code's own)",
		installer.MinAutoCompact, installer.MaxAutoCompact))
	configDir := fs.String("config-dir", "", "OpenCode config directory")
	claudeDir := fs.String("claude-dir", "", "Claude Code config directory (default $CLAUDE_CONFIG_DIR or ~/.claude)")
	deliverables := fs.String("deliverables-dir", "", "folder for agent artifacts (default ~/agent-deliverables)")
	sdk := fs.String("android-sdk", "", "Android SDK (default $ANDROID_HOME or the platform default)")
	skipDeps := fs.Bool("skip-deps", false, "do not install Engram or CodeGraph")
	noProfile := fs.Bool("no-shell-profile", false, "do not add ENGRAM_NUDGE_COOLDOWN_SECS to the shell profile")
	noGitBash := fs.Bool("no-git-bash", false, "Windows: do not set Git Bash as the OpenCode shell")
	dryRun := fs.Bool("dry-run", false, "show what would change without writing")
	_ = fs.Parse(args)

	comps := installer.DefaultComponents()
	comps.Persona, comps.Concise, comps.SpecLanguage = *persona, *concise, *specLanguage
	for _, w := range with {
		switch w {
		case "subagent-monitor":
			comps.SubagentMonitor = true
		case "claude-sandbox":
			comps.ClaudeSandbox = true
		default:
			fail(fmt.Errorf("unknown component %q for --with", w))
		}
	}
	for _, w := range without {
		switch w {
		case "sdd":
			comps.SDD = false
		case "android":
			comps.Android = false
		case "skills":
			comps.Skills = false
		case "plugin":
			comps.Plugin = false
		case "engram":
			comps.Engram = false
		case "codegraph":
			comps.CodeGraph = false
		default:
			fail(fmt.Errorf("unknown component %q", w))
		}
	}
	opts := installer.Options{Targets: targets, Models: models, Efforts: efforts, Components: comps,
		ClaudeAutoCompact: *autoCompact, ConfigDir: *configDir, ClaudeDir: *claudeDir, DeliverablesDir: *deliverables, AndroidSDK: *sdk,
		SkipDeps: *skipDeps, NoShellProfile: *noProfile, NoGitBash: *noGitBash, DryRun: *dryRun}
	runInstall(opts)
}

var statusMark = map[installer.Status]string{installer.Present: "✓", installer.WillInstall: "↓",
	installer.Optional: "!", installer.Blocking: "✗"}

// printRequirements lists the tools the selection needs before anything is written.
func printRequirements(opts installer.Options) {
	if len(opts.Targets) == 0 {
		opts.Targets = installer.DetectTargets(opts)
	}
	fmt.Println("Required tools:")
	for _, r := range installer.Requirements(opts) {
		line := fmt.Sprintf("  %s %-20s %s", statusMark[r.Status], r.Name, r.Purpose)
		switch r.Status {
		case installer.Present:
			line += " (" + r.Detail + ")"
		case installer.WillInstall:
			line += ": will be installed " + r.Detail
		default:
			line += ": missing: " + r.Hint
		}
		fmt.Println(line)
	}
	fmt.Println()
}

func runInstall(opts installer.Options) {
	if opts.DryRun {
		fmt.Println("Dry run: nothing is written.")
	}
	printRequirements(opts)
	res, err := installer.Install(opts, cliLogger{})
	if err != nil {
		fail(err)
	}
	fmt.Println("\nDone. Next steps:")
	for i, s := range res.NextSteps {
		fmt.Printf("  %d. %s\n", i+1, s)
	}
	if res.BackupDir != "" {
		fmt.Println("Backups of replaced files: " + res.BackupDir)
	}
}

func syncCmd(args []string) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "show what would change without writing")
	_ = fs.Parse(args)
	opts, err := installer.ReadSelection("")
	if err != nil {
		fail(err)
	}
	opts.DryRun = *dryRun
	runInstall(*opts)
}

func uninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	var targets listFlag
	fs.Var(&targets, "targets", "assistants to remove (default: every installed one)")
	dryRun := fs.Bool("dry-run", false, "show what would change without writing")
	_ = fs.Parse(args)
	if err := installer.Uninstall(installer.Options{Targets: targets, DryRun: *dryRun}, cliLogger{}); err != nil {
		fail(err)
	}
}
