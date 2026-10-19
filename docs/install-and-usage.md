# Install and usage guide

← [Back to README](../README.md)

This guide covers macOS (Apple Silicon and Intel) and Windows. Commands are the same on both unless
a section says otherwise; on Windows, run them in PowerShell.

- [Before you start](#before-you-start)
- [Install](#install)
- [First run](#first-run)
- [Use the agents](#use-the-agents)
- [Save tokens day to day](#save-tokens-day-to-day)
- [Keep it up to date](#keep-it-up-to-date)
- [Coming from agent-ecosystem](#coming-from-agent-ecosystem)
- [Check your install](#check-your-install)
- [Uninstall](#uninstall)
- [Troubleshooting](#troubleshooting)

---

## Before you start

### macOS

You need:

- **git**. If it is missing, install the Xcode command line tools with `xcode-select --install`,
  or run `brew install git`.
- **At least one assistant**: [OpenCode](https://opencode.ai/docs) or
  [Claude Code](https://docs.claude.com/en/docs/claude-code/setup). The installer configures the
  assistants you have; it never installs them.
- **Node.js 18+ and npm**, only if you want CodeGraph: `brew install node`.

[Homebrew](https://brew.sh) is recommended. With it, the installer also installs Engram for you.

### Windows

Windows installs from source, so you need Go:

```powershell
winget install Git.Git
winget install GoLang.Go
winget install OpenJS.NodeJS.LTS   # only if you want CodeGraph
```

Git for Windows also brings Git Bash, which the agents' shell commands need. Install at least one
assistant too: [OpenCode](https://opencode.ai/docs) or
[Claude Code](https://docs.claude.com/en/docs/claude-code/setup).

Open a new terminal afterwards, so the new `PATH` is loaded.

## Install

### macOS: Homebrew (recommended)

```bash
brew install --cask iesedobleac/tap/iesedoble-ai
```

The binary is not notarized by Apple; the cask removes the quarantine flag on install, so macOS
runs it without a warning.

### macOS: install script

```bash
curl -fsSL https://raw.githubusercontent.com/iesedobleac/iesedoble-ai/main/scripts/install.sh | bash
```

On macOS with Homebrew the script installs the cask above. To install the binary instead,
downloaded from GitHub Releases and checked against `checksums.txt`:

```bash
curl -fsSL https://raw.githubusercontent.com/iesedobleac/iesedoble-ai/main/scripts/install.sh | bash -s -- --method binary
```

The binary goes to `/usr/local/bin` when it is writable, otherwise to `~/.local/bin`; choose
another folder with `--dir <folder>`. If that folder is not in your `PATH`, the script tells you
the line to add.

### Windows: install script

```powershell
irm https://raw.githubusercontent.com/iesedobleac/iesedoble-ai/main/scripts/install.ps1 | iex
```

The script checks git and Go, then builds and installs the binary with `go install`. If Go's
binary folder (`%USERPROFILE%\go\bin`) is not in your `PATH`, it prints the command to add it;
run it and open a new terminal.

### From source

With Go 1.26 or later, on either system:

```bash
go install github.com/iesedobleac/iesedoble-ai/cmd/iesedoble-ai@latest
```

Check the install:

```bash
iesedoble-ai version
```

## First run

```bash
iesedoble-ai
```

Choose **Install or update** and go through the six steps. Move with `↑`/`↓`, toggle with
`space`, continue with `enter`, go back with `esc`.

1. **Assistants.** The ones found on this machine are marked as detected and preselected.
2. **Components.** What gets installed; all are selected by default:

   | Component | What it adds |
   |---|---|
   | SDD flow | `sdd-orchestrator`, its phase agents and the `sdd-workflow` skill |
   | Android orchestrator | `android-orchestrator` (its subagents also come with the SDD flow) |
   | General skills | `skill-generator` |
   | Context warning | In OpenCode, past 200k tokens of context (then every +100k), asks whether to compact now, start a new session with the same agent and model, or keep going |
   | Engram memory | Persistent memory MCP server and plugin |
   | CodeGraph | Code graph MCP server |

3. **Persona.** How your assistant talks to you, added once to `AGENTS.md` / `CLAUDE.md`. A panel
   explains the option under the cursor, with what it changes and what it costs:

   | Option | What it does |
   |---|---|
   | Architect (default) | Verifies before agreeing, corrects you with evidence, asks one question at a time and teaches in depth only when you ask (~300 tokens) |
   | None | Keeps your assistant's own voice; adds nothing |
   | Concise (on by default) | Caveman-style replies: answer first, no filler, code and errors verbatim, full sentences for warnings and questions. About 600 tokens of rules that cut output tokens, the expensive ones |

4. **Models.** One model per role and assistant. A panel explains the role under the cursor and
   what suits it; pick whatever you prefer. Press `enter` on a row to choose from the models the
   assistant offers, and type to filter. OpenCode lists the models of the providers you have
   connected; Claude Code offers `opus`, `sonnet`, `haiku`, `fable` and `inherit`. **Custom…**
   accepts any other model ID. In Claude Code, `←`/`→` changes the role's effort.

   | Role | Agents | What suits it |
   |---|---|---|
   | planner | Orchestrators, `sdd-spec`, `sdd-clarify`, `sdd-plan` | Strongest model, high effort: a bad plan compounds into a bad implementation |
   | analyst | `sdd-discovery`, `sdd-constitution`, `sdd-tasks`, `android-research` | Balanced model, medium effort |
   | executor | `android-worker`, `sdd-apply` | Balanced model, low effort; the cheapest only for trivial edits |
   | scout | `android-explore`, `android-device` | Fastest, cheapest model, low effort |
   | reviewer | `android-review`, `sdd-verify` | Strong model, medium effort; another model family than the executor catches different mistakes |

   Effort is reasoning, billed as output tokens: `low` uses roughly a third of `medium`, and
   `high` several times more. `auto` keeps your session's level.
5. **Settings.** How each selected assistant behaves. `space` or `←`/`→` changes the row under
   the cursor, and a panel explains it.

   | Assistant | Setting | What it does |
   |---|---|---|
   | Claude Code | Sandbox | Off by default. Turns on Claude Code's sandbox: shell commands write only inside the project and have no network, so they run without permission prompts. `adb` and `./gradlew` run outside it and ask as usual. Needs macOS, Linux (with `bubblewrap` and `socat`) or WSL2. Run `/sandbox` in Claude Code to check it |
   | Claude Code | Auto-compact | How many tokens of context Claude Code allows before it summarizes the conversation on its own: Claude Code's default (near the model's limit), 100k, 150k, 200k, 300k or 500k. A smaller window keeps sessions cheaper and focused. Saved as `autoCompactWindow` in `settings.json`; `/autocompact` still overrides it per model |
   | OpenCode | Subagent monitor | Off by default. Keeps the subagent statusline that Engram's setup adds; off removes it from `tui.json` and `cli.json`, after a backup |
6. **Review.** Check the summary and choose **Install**, or **Dry run** to see what would change
   without writing anything.

Before writing, the installer shows the tools your selection needs. `✓` is present, `↓` will be
installed for you (Engram and CodeGraph), `!` is missing but optional, and `✗` stops the install
until you install it.

At the end you get the next steps for each assistant.

On Windows, Engram is downloaded from its GitHub release to `%USERPROFILE%\bin`. If the installer
warns that this folder is not in your `PATH`, add it and open a new terminal:

```powershell
[Environment]::SetEnvironmentVariable('Path', $env:Path + ";$env:USERPROFILE\bin", 'User')
```

### Without the menu

The same install, from the command line:

```bash
iesedoble-ai install                       # detected assistants, everything, default models
iesedoble-ai install --dry-run             # show what would change
iesedoble-ai install --targets claude --without android,codegraph
iesedoble-ai install --with subagent-monitor,claude-sandbox
iesedoble-ai install --persona none --concise=false   # assistant's default voice
iesedoble-ai install --planner openai/gpt-5.5 --claude-reviewer opus
iesedoble-ai install --claude-executor haiku --claude-executor-effort low
```

Run `iesedoble-ai install --help` for every flag. A model the assistant does not list produces
a warning and the install goes on.

## Use the agents

### OpenCode

1. Restart OpenCode. If it runs as a background service, also run `opencode service restart` from
   a new terminal.
2. Connect your model provider with `/connect`, if you have not yet.
3. Pick `sdd-orchestrator` or `android-orchestrator` as the agent and describe what you need.

### Claude Code

Start a session with an orchestrator as the main agent:

```bash
claude --agent sdd-orchestrator
claude --agent android-orchestrator
```

The other twelve agents are subagents: the orchestrators delegate to them.

### CodeGraph

Index each repository once, from its root:

```bash
codegraph init
```

### Which orchestrator

- **`sdd-orchestrator`** for Spec-Driven Development: project discovery, a constitution,
  functional specs, a technical plan, tasks, implementation one task at a time, and verification.
  Each phase runs when you ask for it, and nothing is saved without your approval.
- **`android-orchestrator`** for small and medium Android tasks without SDD phases.

## Save tokens day to day

Every turn sends the whole conversation to the model again: files read, command output, earlier
answers. Caching makes repeats cheaper, but a long session still costs more on every message and
fills the context. These habits save more than any setting:

| Habit | Claude Code | OpenCode |
|---|---|---|
| One task, one session: start fresh when you change topic | `/clear` | `ctrl+x n` (new session) |
| Compact a long session: summarizes the history, keeps the thread | `/compact` | `/compact` or `ctrl+x c` |
| See what uses the context and what it costs | `/context`, `/usage` | `opencode stats --cost` |
| Less reasoning for simple tasks | `/effort low` | a model variant, such as `provider/model#low` |
| Let subagents explore: their context stays out of your session | the orchestrators already delegate | same |

In OpenCode, the **Context warning** plugin does the reminding: past 200k tokens it asks whether
to compact now or continue in a new session, and asks again every 100k if you keep going. What
the agents saved to Engram is still there in the new session.

Claude Code has no such dialog, but it compacts on its own: choose its auto-compact window on the
Settings step (200k compacts about where the OpenCode warning asks).

## Keep it up to date

macOS:

```bash
brew upgrade --cask iesedobleac/tap/iesedoble-ai
iesedoble-ai sync
```

Windows:

```powershell
go install github.com/iesedobleac/iesedoble-ai/cmd/iesedoble-ai@latest
iesedoble-ai sync
```

`sync` re-applies your last selection with the new version. Files you edited are backed up
before they are replaced.

On macOS, if you installed with the script's binary method, run the script again instead of
`brew upgrade`. Restart OpenCode afterwards (`opencode service restart`).

## Coming from agent-ecosystem

`iesedoble-ai` was called `agent-ecosystem` before v0.4.0.

macOS: Homebrew moves the old cask to the new name. The new cask has to be trusted first:

```bash
brew update
brew trust --cask iesedobleac/tap/iesedoble-ai
brew upgrade --cask agent-ecosystem
iesedoble-ai sync
```

After the upgrade, `agent-ecosystem` is an alias of `iesedoble-ai` in Homebrew: do not run
`brew uninstall --cask agent-ecosystem`, as it would uninstall `iesedoble-ai`.

Windows:

```powershell
go install github.com/iesedobleac/iesedoble-ai/cmd/iesedoble-ai@latest
iesedoble-ai sync
```

Then delete the old `agent-ecosystem.exe` from `%USERPROFILE%\go\bin`.

The first `sync` moves `~/.agent-ecosystem` (state, backups and logs) to `~/.iesedoble-ai`, and
replaces the `agent-ecosystem` blocks in `AGENTS.md`, `CLAUDE.md` and your shell profile with the
new ones, without duplicating them.

## Check your install

On macOS:

```bash
iesedoble-ai doctor
```

It only reads: the tools, the recorded install, and whether installed files are missing or were
edited by hand.

## Uninstall

On macOS:

```bash
iesedoble-ai uninstall
brew uninstall --cask iesedoble-ai
```

`uninstall` removes what it added: agents, skills, the rules block in `AGENTS.md` / `CLAUDE.md`,
its MCP servers and the Claude Code deny rules, and with the last assistant also the line it added
to your shell profile. Engram, CodeGraph and the assistants stay installed. To remove a single
assistant, pass `--targets opencode` or `--targets claude`; add `--dry-run` to see what would be
removed.

## Troubleshooting

**The models step shows "choose a model" for OpenCode, or no models at all.**
OpenCode only lists the models of connected providers. Connect one in OpenCode with `/connect`
and run `iesedoble-ai` again, or use **Custom…** and type the model ID. When the models cannot
be listed, the menu and `iesedoble-ai install` show why, with OpenCode's own error.

**An OpenCode subagent statusline still shows up.**
Engram's setup adds it; the installer removes it unless **Subagent monitor** is selected. Run
`iesedoble-ai sync`, then `opencode service restart`, and check with `opencode plugin list`.

**The install stops at "Required tools".**
A tool marked `✗` is missing: install it with the hint shown next to it and run again. **Dry run**
still works, to see what would change.

**Something failed and the message is gone.**
Every install, sync and uninstall writes a log to `~/.iesedoble-ai/logs/`, with your selection,
each step and every warning; the path is shown at the end of the run. Dry runs write nothing.

**A file I changed was replaced.**
It was backed up first. Backups are in `~/.iesedoble-ai/backups/<date>/`, with the same path
as the original.

**My comments in `opencode.jsonc` are gone.**
The installer rewrites that file as plain JSON. The original, with comments, is in the backups
folder.

**Where things are** (on Windows, `~` is your user folder, `%USERPROFILE%`)

| What | Where |
|---|---|
| Install record and last selection | `~/.iesedoble-ai/state.json` |
| Backups | `~/.iesedoble-ai/backups/` |
| Logs of each install, sync and uninstall (the last 20) | `~/.iesedoble-ai/logs/` |
| OpenCode files | `~/.config/opencode` (or `--config-dir`) |
| Claude Code files | `~/.claude`, or `$CLAUDE_CONFIG_DIR` (or `--claude-dir`) |
| Agent deliverables | `~/agent-deliverables` (or `--deliverables-dir`) |
