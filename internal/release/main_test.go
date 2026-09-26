package main

import (
	"strings"
	"testing"
)

const sample = `# Changelog

## [Unreleased]

### Added
- Hive stats.

## [1.1.0] - 2026-09-01

### Fixed
- Honey rounding.

## [1.0.0] - 2026-08-01

- First release.
`

func TestBump(t *testing.T) {
	v := version{1, 2, 3}
	for kind, want := range map[string]string{"patch": "1.2.4", "minor": "1.3.0", "major": "2.0.0", "3.0.1": "3.0.1"} {
		got, err := bump(v, kind)
		if err != nil || got.String() != want {
			t.Errorf("bump %s = %s, %v", kind, got, err)
		}
	}
	if _, err := bump(v, "huge"); err == nil {
		t.Error("unknown bump accepted")
	}
	if !(version{1, 9, 0}).less(version{1, 10, 0}) {
		t.Error("versions must compare numerically")
	}
}

func TestChangelog(t *testing.T) {
	if v, err := currentVersion(sample); err != nil || v != "1.1.0" {
		t.Errorf("current = %s, %v", v, err)
	}
	notes, err := changelogSection(sample, "1.1.0")
	if err != nil || notes != "### Fixed\n- Honey rounding." {
		t.Errorf("notes = %q, %v", notes, err)
	}
	if notes, _ := changelogSection(sample, "1.0.0"); notes != "- First release." {
		t.Errorf("last section = %q", notes)
	}
	if _, err := changelogSection(sample, "9.9.9"); err == nil {
		t.Error("missing section accepted")
	}
	text, err := addSection(sample, "1.2.0", "2026-10-01")
	if err != nil || !strings.Contains(text, "## [Unreleased]\n\n## [1.2.0] - 2026-10-01\n\n### Added\n- Hive stats.") {
		t.Errorf("addSection = %s, %v", text, err)
	}
	if _, err := addSection("# Changelog", "1.0.0", "x"); err == nil {
		t.Error("changelog without Unreleased accepted")
	}
}

func TestCheckBranch(t *testing.T) {
	tests := []struct {
		base, head, title string
		ok                bool
	}{
		{"main", "release/1.2.0", "", true},
		{"main", "hotfix/1.2.1", "", true},
		{"main", "feature/12-stats", "", false},
		{"main", "develop", "", false},
		{"develop", "feature/12-hive-stats", "", true},
		{"develop", "fix/7-honey", "", true},
		{"develop", "main", "", true},
		{"develop", "release/1.2.0", "", true},
		{"develop", "dependabot/go_modules/x", "", true},
		{"develop", "feature/hive-stats", "", false},
		{"develop", "feature/12_Stats", "", false},
		{"develop", "claude/tender-bell", "Bootstrap (#1)", true},
		{"develop", "claude/tender-bell", "Bootstrap", false},
		{"other", "anything", "", true},
	}
	for _, tt := range tests {
		err := checkBranch(tt.base, tt.head, tt.title)
		if (err == nil) != tt.ok {
			t.Errorf("%s → %s (%q): %v", tt.head, tt.base, tt.title, err)
		}
	}
}
