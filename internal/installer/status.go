package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Repository links shown by the menu.
const (
	RepoURL  = "https://github.com/iesedobleac/iesedoble-ai"
	GuideURL = RepoURL + "/blob/main/docs/install-and-usage.md"
)

// releaseURL is the GitHub API endpoint of the latest release; replaced in tests.
var releaseURL = "https://api.github.com/repos/iesedobleac/iesedoble-ai/releases/latest"

// InstallStatus summarizes the recorded install for the menu.
type InstallStatus struct {
	Targets   []string  // installed assistants, in display order
	Installed time.Time // most recent install or sync
	Version   string    // version that made that install
}

// ReadStatus reads the recorded install; nil when there is none.
func ReadStatus(home string) *InstallStatus {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	data, err := os.ReadFile(filepath.Join(stateDirFor(home), "state.json"))
	if err != nil {
		return nil
	}
	var s State
	if json.Unmarshal(data, &s) != nil || len(s.Targets) == 0 {
		return nil
	}
	st := &InstallStatus{}
	for _, id := range sortedKeys(s.Targets) {
		st.Targets = append(st.Targets, id)
		t := s.Targets[id]
		if when, err := time.Parse("2006-01-02T15-04-05Z", t.Installed); err == nil && when.After(st.Installed) {
			st.Installed, st.Version = when, t.Version
		}
	}
	sort.SliceStable(st.Targets, func(i, j int) bool { return rank(st.Targets[i]) < rank(st.Targets[j]) })
	return st
}

func rank(id string) int {
	for i, t := range AllTargets {
		if t == id {
			return i
		}
	}
	return len(AllTargets)
}

// LatestVersion asks GitHub for the latest release, without the leading "v".
func LatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return strings.TrimPrefix(release.Tag, "v"), nil
}

// NewerVersion reports whether latest is a higher x.y.z version than current.
// Versions that do not parse (such as "dev") are never older.
func NewerVersion(current, latest string) bool {
	c, okC := parseVersion(current)
	l, okL := parseVersion(latest)
	if !okC || !okL {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// SelfUpgrade is the command that upgrades this binary in place, or nil when
// it was not installed in a way the menu can upgrade (only Homebrew for now).
func SelfUpgrade() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if strings.Contains(filepath.ToSlash(exe), "/Caskroom/"+toolName+"/") {
		return []string{"brew", "upgrade", "--cask", toolName}
	}
	return nil
}

// UpgradeCommand is how to install a newer version on this system.
func UpgradeCommand() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew upgrade --cask iesedoble-ai"
	case "windows":
		return "go install github.com/iesedobleac/iesedoble-ai/cmd/iesedoble-ai@latest"
	}
	return "curl -fsSL https://raw.githubusercontent.com/iesedobleac/iesedoble-ai/main/scripts/install.sh | bash"
}
