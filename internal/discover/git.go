package discover

import (
	"os/exec"
	"strings"
)

func enrichGit(m *Module) {
	run := func(args ...string) (string, bool) {
		cmd := exec.Command("git", append([]string{"-C", m.Dir}, args...)...)
		out, err := cmd.Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	if commit, ok := run("rev-parse", "HEAD"); ok {
		m.Commit = commit
	}
	if tag, ok := run("describe", "--tags", "--always", "--dirty"); ok {
		if tag != "" {
			m.Version = tag
		}
		m.Dirty = strings.HasSuffix(tag, "-dirty")
	}
}
