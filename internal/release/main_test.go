package main

import (
	"os"
	"os/exec"
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

// repo creates a work tree with a bare origin that has main and develop,
// a CHANGELOG and the given tags, and makes it the working directory.
func repo(t *testing.T, tags ...string) {
	t.Helper()
	root := t.TempDir()
	origin, work := root+"/origin.git", root+"/work"
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(root, "init", "-q", "--bare", "-b", "main", origin)
	git(root, "init", "-q", "-b", "main", work)
	write := func(name, text string) {
		if err := os.WriteFile(work+"/"+name, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/hive\n\ngo 1.24\n")
	write("CHANGELOG.md", sample)
	git(work, "add", ".")
	git(work, "commit", "-q", "-m", "init")
	for _, tag := range tags {
		git(work, "tag", tag)
	}
	git(work, "remote", "add", "origin", origin)
	git(work, "push", "-q", "origin", "main", "--tags")
	git(work, "push", "-q", "origin", "main:develop")
	t.Chdir(work)
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("RELEASE_SKIP_GORELEASE", "1")
}

func gitOut(t *testing.T, args ...string) string {
	t.Helper()
	out, err := run("git", args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func TestLatestTag(t *testing.T) {
	repo(t, "v1.0.0", "v1.10.0", "v1.9.0", "v2.0.0-rc.1", "release-x")
	v, found, err := latestTag()
	if err != nil || !found || v.String() != "1.10.0" {
		t.Fatalf("latestTag = %s %v %v", v, found, err)
	}
}

func TestStart(t *testing.T) {
	repo(t, "v1.1.0")
	if err := start([]string{"minor"}); err != nil {
		t.Fatal(err)
	}
	if b := gitOut(t, "branch", "--show-current"); b != "release/1.2.0" {
		t.Errorf("branch = %s", b)
	}
	if msg := gitOut(t, "log", "-1", "--format=%s"); msg != "chore(release): 1.2.0" {
		t.Errorf("commit = %s", msg)
	}
	text, _ := os.ReadFile(changelog)
	if !strings.Contains(string(text), "## [Unreleased]\n\n## [1.2.0] - ") {
		t.Errorf("CHANGELOG:\n%s", text)
	}
}

func TestStartHotfix(t *testing.T) {
	repo(t, "v1.1.0")
	if err := start([]string{"patch", "--hotfix"}); err != nil {
		t.Fatal(err)
	}
	if b := gitOut(t, "branch", "--show-current"); b != "hotfix/1.1.1" {
		t.Errorf("branch = %s", b)
	}
}

func TestStartErrors(t *testing.T) {
	repo(t, "v1.1.0")
	if err := start([]string{"1.0.0"}); err == nil || !strings.Contains(err.Error(), "must be greater") {
		t.Errorf("older version: %v", err)
	}
	if err := os.WriteFile("dirty.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := start([]string{"patch"}); err == nil || !strings.Contains(err.Error(), "not clean") {
		t.Errorf("dirty tree: %v", err)
	}
}

func TestCheckRelease(t *testing.T) {
	repo(t, "v1.0.0") // sample CHANGELOG has 1.1.0 and 1.0.0
	tests := []struct {
		head, want string // want: "" for success, else an error substring
	}{
		{"release/1.1.0", ""},
		{"hotfix/1.1.0", ""},
		{"release/1.0.0", "greater than the latest tag"},
		{"release/1.2.0", `no "## [1.2.0]" section`},
		{"feature/3-x", "only release/X.Y.Z"},
	}
	for _, tt := range tests {
		err := check("main", tt.head, "")
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: err = %v, want %q", tt.head, err, tt.want)
		}
	}
	if err := check("", "x", ""); err == nil {
		t.Error("missing BASE_REF accepted")
	}
}

func TestCheckReleaseMajor(t *testing.T) {
	repo(t, "v1.1.0")
	text, _ := os.ReadFile(changelog)
	if err := os.WriteFile(changelog, []byte(strings.Replace(string(text), "## [1.1.0]", "## [2.0.0]", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := check("main", "release/2.0.0", ""); err == nil || !strings.Contains(err.Error(), "/v2") {
		t.Errorf("v2 without /v2 module path: %v", err)
	}
}

func TestCheckTagExists(t *testing.T) {
	repo(t, "v1.0.0", "v1.1.0")
	if err := check("main", "release/1.1.0", ""); err == nil || !strings.Contains(err.Error(), "greater") {
		t.Errorf("existing tag: %v", err)
	}
}
