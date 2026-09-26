// Command release implements the release flow described in CONTRIBUTING.md.
//
//	go run ./internal/release start <patch|minor|major|X.Y.Z> [--hotfix]
//	go run ./internal/release check          # CI: branch and version policy of a pull request
//	go run ./internal/release notes X.Y.Z    # CHANGELOG section of a version
//	go run ./internal/release current        # latest version in CHANGELOG.md
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	changelog  = "CHANGELOG.md"
	unreleased = "## [Unreleased]"
	// gorelease checks that the version number matches the API changes.
	gorelease = "golang.org/x/exp/cmd/gorelease@v0.0.0-20260908205506-85c1c2202aba"
)

var (
	versionRe       = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	releaseBranchRe = regexp.MustCompile(`^(release|hotfix)/(\d+\.\d+\.\d+)$`)
	topicBranchRe   = regexp.MustCompile(`^(feature|fix|docs|refactor|test|ci|chore|perf)/(\d+)-[a-z0-9][a-z0-9-]*$`)
	issueRefRe      = regexp.MustCompile(`#\d+\b`)
	sectionRe       = regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`)
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch args := os.Args[2:]; os.Args[1] {
	case "start":
		err = start(args)
	case "check":
		err = check(os.Getenv("BASE_REF"), os.Getenv("HEAD_REF"), os.Getenv("PR_TITLE"))
	case "notes":
		if len(args) != 1 {
			usage()
		}
		var notes string
		if notes, err = changelogSection(readChangelog(), args[0]); err == nil {
			fmt.Println(notes)
		}
	case "current":
		var v string
		if v, err = currentVersion(readChangelog()); err == nil {
			fmt.Println(v)
		}
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "✖", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: release start <patch|minor|major|X.Y.Z> [--hotfix] | check | notes X.Y.Z | current")
	os.Exit(2)
}

func run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func readChangelog() string {
	b, err := os.ReadFile(changelog)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✖", err)
		os.Exit(1)
	}
	return string(b)
}

type version [3]int

func parseVersion(s string) (version, bool) {
	m := versionRe.FindStringSubmatch(strings.TrimPrefix(s, "v"))
	if m == nil {
		return version{}, false
	}
	var v version
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func (v version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

func (v version) less(w version) bool { return slices.Compare(v[:], w[:]) < 0 }

func bump(v version, kind string) (version, error) {
	if next, ok := parseVersion(kind); ok {
		return next, nil
	}
	switch kind {
	case "major":
		return version{v[0] + 1, 0, 0}, nil
	case "minor":
		return version{v[0], v[1] + 1, 0}, nil
	case "patch":
		return version{v[0], v[1], v[2] + 1}, nil
	}
	return version{}, fmt.Errorf("unknown bump %q; use patch, minor, major or X.Y.Z", kind)
}

// latestTag returns the highest vX.Y.Z tag, or false if there are none.
func latestTag() (version, bool, error) {
	out, err := run("git", "tag", "--list", "v*")
	if err != nil {
		return version{}, false, err
	}
	var latest version
	found := false
	for _, tag := range strings.Fields(out) {
		if v, ok := parseVersion(tag); ok && strings.HasPrefix(tag, "v") && (!found || latest.less(v)) {
			latest, found = v, true
		}
	}
	return latest, found, nil
}

func start(args []string) error {
	if len(args) == 0 {
		usage()
	}
	hotfix := slices.Contains(args[1:], "--hotfix")
	base := "develop"
	if hotfix {
		base = "main"
	}
	if out, err := run("git", "status", "--porcelain"); err != nil || out != "" {
		return errors.New("working tree is not clean")
	}
	if _, err := run("git", "fetch", "origin", base, "--tags"); err != nil {
		return err
	}
	current, _, err := latestTag()
	if err != nil {
		return err
	}
	next, err := bump(current, args[0])
	if err != nil {
		return err
	}
	if !current.less(next) {
		return fmt.Errorf("%s must be greater than the latest tag v%s", next, current)
	}
	kind := "release"
	if hotfix {
		kind = "hotfix"
	}
	branch := kind + "/" + next.String()
	if _, err := run("git", "switch", "-c", branch, "origin/"+base); err != nil {
		return err
	}
	text, err := addSection(readChangelog(), next.String(), time.Now().Format(time.DateOnly))
	if err != nil {
		return err
	}
	if err := os.WriteFile(changelog, []byte(text), 0o644); err != nil {
		return err
	}
	if _, err := run("git", "commit", "-am", "chore(release): "+next.String()); err != nil {
		return err
	}
	fmt.Printf("✔ %s is ready. Review %s, then:\n  git push -u origin %s\n  and open a pull request into main.\n", branch, changelog, branch)
	return nil
}

// addSection turns the Unreleased changes into a section for version.
func addSection(text, version, date string) (string, error) {
	if !strings.Contains(text, unreleased) {
		return "", fmt.Errorf("%s has no %q section", changelog, unreleased)
	}
	return strings.Replace(text, unreleased, fmt.Sprintf("%s\n\n## [%s] - %s", unreleased, version, date), 1), nil
}

func changelogSection(text, version string) (string, error) {
	header := "## [" + version + "]"
	start := strings.Index(text, "\n"+header)
	if start < 0 {
		return "", fmt.Errorf("%s has no %q section", changelog, header)
	}
	body := text[start+1:]
	body = body[strings.IndexByte(body+"\n", '\n')+1:]
	if end := strings.Index(body, "\n## ["); end >= 0 {
		body = body[:end]
	}
	return strings.TrimSpace(body), nil
}

// currentVersion returns the newest released version in the changelog.
func currentVersion(text string) (string, error) {
	m := sectionRe.FindStringSubmatch(text)
	if m == nil {
		return "", fmt.Errorf("%s has no released versions", changelog)
	}
	return m[1], nil
}

// check enforces the branch and version policy of a pull request.
func check(base, head, title string) error {
	if base == "" || head == "" {
		return errors.New("BASE_REF and HEAD_REF must be set")
	}
	fmt.Printf("%s → %s\n", head, base)
	if err := checkBranch(base, head, title); err != nil {
		return err
	}
	if base == "main" {
		if err := checkRelease(head); err != nil {
			return err
		}
	}
	fmt.Println("✔ policy passed")
	return nil
}

func checkBranch(base, head, title string) error {
	switch base {
	case "main":
		if !releaseBranchRe.MatchString(head) {
			return errors.New("only release/X.Y.Z and hotfix/X.Y.Z branches can be merged into main")
		}
	case "develop":
		switch {
		case head == "main", releaseBranchRe.MatchString(head), topicBranchRe.MatchString(head),
			strings.HasPrefix(head, "dependabot/"):
		case issueRefRe.MatchString(title):
			fmt.Println("branch name has no issue number; the title references one")
		default:
			return fmt.Errorf("branch %q must be named <type>/<issue>-<name>, e.g. feature/12-hive-stats, "+
				"or the pull request title must reference an issue (#12)", head)
		}
	}
	return nil
}

func checkRelease(head string) error {
	next, _ := parseVersion(releaseBranchRe.FindStringSubmatch(head)[2])
	latest, found, err := latestTag()
	if err != nil {
		return err
	}
	if found && !latest.less(next) {
		return fmt.Errorf("%s must be greater than the latest tag v%s", next, latest)
	}
	if out, _ := run("git", "tag", "--list", "v"+next.String()); out != "" {
		return fmt.Errorf("tag v%s already exists", next)
	}
	if _, err := changelogSection(readChangelog(), next.String()); err != nil {
		return err
	}
	if next[0] >= 2 {
		mod, err := run("go", "list", "-m")
		if err != nil {
			return err
		}
		if suffix := fmt.Sprintf("/v%d", next[0]); !strings.HasSuffix(mod, suffix) {
			return fmt.Errorf("v%d needs the module path to end with %s (go.mod: %s)", next[0], suffix, mod)
		}
	}
	baseArg := "-base=none"
	if found {
		baseArg = "-base=v" + latest.String()
	}
	if os.Getenv("RELEASE_SKIP_GORELEASE") != "" {
		return nil // tests: gorelease needs the network
	}
	cmd := exec.Command("go", "run", gorelease, baseArg, "-version=v"+next.String())
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gorelease: the version does not match the API changes: %w", err)
	}
	return nil
}
