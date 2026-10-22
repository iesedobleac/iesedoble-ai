package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectTargets returns the assistants found on this machine (the first one if none).
func DetectTargets(opts Options) []string {
	fallback := []string{targets[0].id()}
	r, err := newRun(opts, nopLogger{})
	if err != nil {
		return fallback
	}
	var found []string
	for _, t := range targets {
		if t.detected(r) {
			found = append(found, t.id())
		}
	}
	if len(found) == 0 {
		return fallback
	}
	return found
}

// Install installs or updates the selection for each target.
func Install(opts Options, log Logger) (res *Result, err error) {
	if len(opts.Targets) == 0 {
		opts.Targets = DetectTargets(opts)
	}
	migrateStateDir(opts, log)
	log, closeLog := openRunLog(opts, "install", log)
	defer func() { closeLog(err) }()
	for _, t := range opts.Targets {
		if !contains(AllTargets, t) {
			return nil, fmt.Errorf("unknown target %q (supported: %s)", t, strings.Join(AllTargets, ", "))
		}
	}
	for t, efforts := range opts.Efforts {
		for p, e := range efforts {
			if e != "" && !contains(EffortLevels, e) {
				return nil, fmt.Errorf("%s: unknown effort %q for %s (choose: %s)", TargetLabels[t], e, p, strings.Join(EffortLevels, ", "))
			}
		}
	}
	if w := opts.ClaudeAutoCompact; w != 0 && (w < MinAutoCompact || w > MaxAutoCompact) {
		return nil, fmt.Errorf("auto-compact window %d out of range (%d to %d tokens)", w, MinAutoCompact, MaxAutoCompact)
	}
	if p := opts.Components.SelectedPersona(); !contains(Personas, p) {
		return nil, fmt.Errorf("unknown persona %q (choose: %s)", p, strings.Join(Personas, ", "))
	}
	if l := opts.Components.SelectedSpecLanguage(); !contains(SpecLanguages, l) {
		return nil, fmt.Errorf("unknown spec language %q (choose: %s)", l, strings.Join(SpecLanguages, ", "))
	}
	if blockers := Blockers(checkRequirements(opts)); len(blockers) > 0 {
		var names []string
		for _, b := range blockers {
			names = append(names, b.Name+" ("+b.Hint+")")
		}
		msg := "missing required tools: " + strings.Join(names, "; ")
		if !opts.DryRun {
			return nil, errors.New(msg)
		}
		log.Warn(msg)
	}
	r, err := newRun(opts, log)
	if err != nil {
		return nil, err
	}
	// Steps: models, tools, files, each target's own steps, shell profile, state.
	r.total = 5
	for _, id := range opts.Targets {
		r.total += targetByID(id).steps()
	}
	r.step("Checking models")
	r.checkModels()
	r.step("Installing tools")
	if err := r.ensureDependencies(opts.Targets); err != nil {
		return nil, err
	}
	r.step("Preparing files")
	src, err := r.loadSources()
	if err != nil {
		return nil, err
	}
	if !r.opts.DryRun {
		deliverables := expandHome(r.opts.DeliverablesDir, r.home)
		if err := os.MkdirAll(deliverables, 0o755); err != nil {
			return nil, err
		}
	}
	state := r.readState()
	for _, id := range opts.Targets {
		t := targetByID(id)
		st := state.Targets[id]
		if st == nil {
			st = &TargetState{}
		}
		if err := t.install(r, src, st); err != nil {
			return nil, fmt.Errorf("%s: %w", t.label(), err)
		}
		st.Version, st.Installed = Version, r.stamp
		state.Targets[id] = st
	}
	r.step("Shell profile")
	profile, err := r.installProfile()
	if err != nil {
		return nil, err
	}
	if profile != "" {
		state.Profile = profile
	}
	r.step("Saving state")
	selection := r.opts
	state.Selection = &selection
	if err := r.writeState(state); err != nil {
		return nil, err
	}
	if p, ok := log.(Progress); ok {
		p.Step(r.total, r.total, "Done")
	}

	res = &Result{Targets: opts.Targets}
	for _, id := range opts.Targets {
		res.NextSteps = append(res.NextSteps, targetByID(id).nextStep())
	}
	if r.opts.Components.CodeGraph {
		res.NextSteps = append(res.NextSteps, "Index each repository once: codegraph init (in the repository root).")
	}
	if _, err := os.Stat(r.backup); err == nil {
		res.BackupDir = r.backup
	}
	return res, nil
}

// Uninstall removes what previous installs added, for opts.Targets or every installed target.
func Uninstall(opts Options, log Logger) (err error) {
	migrateStateDir(opts, log)
	log, closeLog := openRunLog(opts, "uninstall", log)
	defer func() { closeLog(err) }()
	r, err := newRun(opts, log)
	if err != nil {
		return err
	}
	for _, t := range targets {
		r.bins[t.binary()] = which(t.binary())
	}
	state := r.readState()
	ids := opts.Targets
	if len(ids) == 0 {
		ids = sortedKeys(state.Targets)
	}
	if len(ids) == 0 {
		return fmt.Errorf("nothing to uninstall")
	}
	for _, id := range ids {
		t := targetByID(id)
		if t == nil {
			return fmt.Errorf("unknown target %q (supported: %s)", id, strings.Join(AllTargets, ", "))
		}
		st := state.Targets[id]
		if st == nil {
			log.Warn(t.label() + " is not installed")
			continue
		}
		if err := t.uninstall(r, st); err != nil {
			return err
		}
		delete(state.Targets, id)
	}
	if len(state.Targets) == 0 {
		if err := r.removeProfile(state.Profile); err != nil {
			return err
		}
		state.Profile, state.Selection = "", nil
	}
	if err := r.writeState(state); err != nil {
		return err
	}
	if _, err := os.Stat(r.backup); err == nil {
		log.OK("Backups: " + r.backup)
	}
	log.Info("Engram, CodeGraph and the assistants themselves were not removed.")
	return nil
}

// Doctor runs read-only checks. It returns false when a check fails.
func Doctor(opts Options, log Logger) bool {
	r, err := newRun(opts, log)
	if err != nil {
		log.Warn(err.Error())
		return false
	}
	ok := true
	data, err := os.ReadFile(r.stateFile())
	var state State
	switch {
	case os.IsNotExist(err):
		log.Warn("No install recorded in " + r.stateFile())
	case err != nil:
		log.Warn("Cannot read " + r.stateFile() + ": " + err.Error())
		ok = false
	case json.Unmarshal(data, &state) != nil:
		log.Warn(r.stateFile() + " is not valid JSON; restore a backup or repair it")
		ok = false
	default:
		log.OK("State: " + r.stateFile())
	}
	installed := sortedKeys(state.Targets)
	comps := DefaultComponents()
	if state.Selection != nil {
		comps = state.Selection.Components
	}
	for _, req := range Requirements(Options{Targets: installed, Components: comps}) {
		switch req.Status {
		case Present:
			log.OK(req.Name + " " + req.Detail)
		case WillInstall:
			log.Warn(req.Name + " missing: `iesedoble-ai sync` installs it " + req.Detail)
		default:
			log.Warn(req.Name + " missing: " + req.Hint)
			ok = ok && req.Status != Blocking
		}
	}
	for _, t := range installed {
		st := state.Targets[t]
		missing, changed := 0, 0
		for rel, hash := range st.Files {
			data, err := os.ReadFile(filepath.Join(st.Dir, filepath.FromSlash(rel)))
			if err != nil {
				missing++
			} else if sum(string(data)) != hash {
				changed++
			}
		}
		msg := fmt.Sprintf("%s: %d files in %s (version %s)", TargetLabels[t], len(st.Files), st.Dir, st.Version)
		switch {
		case missing > 0:
			log.Warn(fmt.Sprintf("%s, %d missing; run `iesedoble-ai sync`", msg, missing))
			ok = false
		case changed > 0:
			log.Warn(fmt.Sprintf("%s, %d edited by hand (sync backs them up before replacing)", msg, changed))
		default:
			log.OK(msg)
		}
	}
	return ok
}

type nopLogger struct{}

func (nopLogger) Info(string) {}
func (nopLogger) OK(string)   {}
func (nopLogger) Warn(string) {}
