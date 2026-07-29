# Contributing to go-dicom

Thank you for your interest in contributing to go-dicom! This document provides guidelines and instructions for contributing.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork:
   ```bash
   git clone https://github.com/<your-username>/go-dicom.git
   cd go-dicom
   ```
3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/amrshadid/go-dicom.git
   ```
4. Create a feature branch:
   ```bash
   git checkout -b feature/my-feature
   ```

## Development Environment

### Prerequisites

- **Go 1.22+** - [Download Go](https://go.dev/dl/)
- **golangci-lint v2** - [Install](https://golangci-lint.run/usage/install/). `.golangci.yml` uses the v2 configuration format, so a v1 binary will refuse to read it. CI pins v2.12.2.
- **Make** (optional)

### Setup

```bash
go mod tidy
go build ./...

# Install pre-commit hook (runs fmt, vet, tests, lint before each commit)
git config core.hooksPath .githooks
```

## Running Tests

```bash
# Run all tests
go test ./...

# Run with race detection
go test -v -race ./...

# Generate coverage report
go test -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Or use Make
make test
make test-cover
```

## Running Linter

```bash
golangci-lint run
# or
make lint
```

## Code Style

- Follow standard [Go conventions](https://go.dev/doc/effective_go) and [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Format code with `gofmt -s -w .`
- Manage imports with `goimports`
- Add doc comments for all exported types and functions
- Handle errors explicitly
- Write table-driven tests where appropriate

## Commit Messages

This project follows [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>
```

### Types

- **feat**: New feature
- **fix**: Bug fix
- **docs**: Documentation changes
- **test**: Adding or updating tests
- **refactor**: Code refactoring
- **chore**: Maintenance tasks
- **perf**: Performance improvements

### Examples

```
feat(anonymize): add support for Clean Pixel Data profile
fix(dataset): handle nil pointer when reading empty sequences
docs(readme): update installation instructions
test(tag): add benchmarks for private tag lookup
```

## Pull Request Process

1. Ensure your branch is up to date: `git fetch upstream && git rebase upstream/main`
2. All tests pass: `go test ./...`
3. Linter passes: `golangci-lint run`
4. Code is formatted: `gofmt -s -w .`
5. Open a PR against `main` with a clear description
6. Address review feedback

### PR Guidelines

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if the public API changes
- Do not include unrelated formatting fixes
- **Never include Protected Health Information (PHI)** in code, tests, or issues

## Reporting Issues

### Bug Reports

Use the bug report template. Include:
- Steps to reproduce
- Go version and OS
- Sample data (anonymized, never real patient data)

### Feature Requests

Use the feature request template. Describe the use case and proposed approach.

## Releasing

Releases are cut by pushing a tag. The `build-release.yml` workflow does the
rest: it cross-compiles the CLI for five platforms, generates `SHA256SUMS`,
extracts the release notes from `CHANGELOG.md`, and publishes the GitHub
release with the binaries attached.

1. **Update `CHANGELOG.md`.** Add a section headed exactly `## [X.Y.Z] - YYYY-MM-DD`
   and a comparison link at the bottom of the file. The workflow extracts the
   release notes by matching this heading, so the format matters.
2. **Update `Version` in `main.go`** to `X.Y.Z`.
3. **Merge to `main`** via a pull request, and wait for CI to pass.
4. **Tag the merge commit on `main`:**
   ```bash
   git checkout main && git pull
   git tag -a vX.Y.Z -m "vX.Y.Z — short summary"
   git push origin vX.Y.Z
   ```
5. **Watch the release workflow.** It publishes the release itself.

### Tag naming

The tag **must** be `vX.Y.Z` — a leading `v` followed by valid
[semver](https://semver.org/), with no other punctuation.

Go's module proxy silently ignores tags that are not valid semver. A tag like
`v.1.2.1` (note the extra dot) looks fine in the GitHub UI but is invisible to
`go get`, so nobody can install that version. Verify after tagging:

```bash
go list -m -versions github.com/amrshadid/go-dicom
```

The new version must appear in that list. If it does not, the tag name is wrong.

### Do not create the release by hand

The workflow owns the GitHub release. Creating one manually with
`gh release create` before the workflow finishes causes it to be overwritten —
the action replaces the name and body of an existing release with its own.

If the notes need changing, edit `CHANGELOG.md` and re-run the workflow, or
edit the release in the GitHub UI *after* the workflow has completed.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
