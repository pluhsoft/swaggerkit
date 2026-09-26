# Contributing

## Branches

Git Flow. Every branch starts from an issue.

| Branch                   | From      | Into      | Purpose                                   |
| ------------------------ | --------- | --------- | ----------------------------------------- |
| `develop`                | —         | —         | Development, default branch               |
| `<type>/<issue>-<name>`  | `develop` | `develop` | One issue, e.g. `feature/12-hive-stats`   |
| `release/X.Y.Z`          | `develop` | `main`    | Release                                   |
| `hotfix/X.Y.Z`           | `main`    | `main`    | Urgent fix of a released version          |
| `main`                   | —         | —         | Released code, every commit is tagged     |

`<type>`: `feature`, `fix`, `docs`, `refactor`, `test`, `ci`, `chore`, `perf`.
Branches of bots and AI agents (`claude/*`) are accepted when the pull request title references the issue: `Add hive stats (#12)`.

## Everyday work

```sh
git switch develop && git pull
git switch -c feature/12-hive-stats
# code, tests, docs
go test ./...
```

1. Add a line to `## [Unreleased]` in [CHANGELOG.md](CHANGELOG.md).
2. Open a pull request into `develop`, reference the issue (`Closes #12`).
3. Merge when CI is green.

## Checks

Run before pushing:

```sh
gofmt -l .                     # must print nothing
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
go test -race ./...            # library coverage must stay ≥ 90%
go test ./examples/apiary -run TestOpenAPI -update   # after API changes in the example
```

CI (`verify`) also runs `go mod tidy -diff`, `govulncheck` and tests on Go 1.24, oldstable and stable.

## Release

Versions are git tags `vX.Y.Z` ([SemVer](https://semver.org)): patch for fixes, minor for features, major for breaking changes.

```sh
go run ./internal/release start minor    # or patch | major | 1.2.0; creates release/X.Y.Z from develop
git push -u origin release/1.2.0
```

Open a pull request `release/X.Y.Z` → `main` and merge it with a **merge commit**. The `version-policy` check requires:

- version greater than the latest tag, tag not taken yet;
- a `## [X.Y.Z]` section in CHANGELOG.md;
- a version bump that matches the API changes ([gorelease](https://pkg.go.dev/golang.org/x/exp/cmd/gorelease));
- for v2+, a module path ending with `/vN`.

After the merge, [`release.yml`](.github/workflows/release.yml) runs CI, creates the tag and GitHub release, publishes the version to `proxy.golang.org` (pkg.go.dev picks it up) and opens a backmerge pull request `main` → `develop`. Merge it with a merge commit.
[`pages.yml`](.github/workflows/pages.yml) publishes the documentation site.

Hotfix: `go run ./internal/release start patch --hotfix`, pull request into `main`, the rest is the same.

A published tag cannot be changed: the Go proxy caches it forever. To withdraw a broken version, add `retract vX.Y.Z` to `go.mod` and release the next version.

## One-time repository setup

1. **Settings → General**: repository name `swaggerkit`; default branch `develop`; allow merge commits and squash merging; enable *Automatically delete head branches*.
2. **Settings → Actions → General → Workflow permissions**: *Read repository contents*; enable *Allow GitHub Actions to create and approve pull requests* (backmerge).
3. **Settings → Rules → Rulesets → Import a ruleset**: [`main.json`](.github/rulesets/main.json), [`develop.json`](.github/rulesets/develop.json), [`tags.json`](.github/rulesets/tags.json). Branches need a pull request (0 approvals) and the `verify` and `version-policy` checks; release tags cannot be moved or deleted.
4. **Settings → Pages → Source**: *GitHub Actions*.
5. **Settings → Code security**: enable *Private vulnerability reporting* and Dependabot alerts.
