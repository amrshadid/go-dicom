package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A release merged into main stayed unpublished until someone tagged the
// right commit by hand (#287). The release workflow now decides for itself,
// with scripts/release-tag.sh, and the decision is what must not be wrong:
// an ordinary push to main must release nothing, and a release must not be
// missed. It is tested here, on repositories made for the purpose, because
// the workflow cannot be run without publishing.

// releaseRepo makes a repository whose main.go declares version, whose
// changelog has sections for the versions in changelog, and which has the
// given tags.
func releaseRepo(t *testing.T, version string, changelog, tags []string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	notes := "# Changelog\n\n## [Unreleased]\n"
	for _, v := range changelog {
		notes += "\n## [" + v + "] - 2026-01-01\n\n- something\n"
	}
	write("main.go", "package main\n\nvar Version = \""+version+"\"\n")
	write("CHANGELOG.md", notes)
	git("init", "-q")
	git("add", "-A")
	git("commit", "-q", "-m", "release")
	for _, tag := range tags {
		git("tag", tag)
	}
	return dir
}

// decide runs the script in dir and returns its three outputs.
func decide(t *testing.T, dir string, args ...string) (tag, build, publish string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("scripts", "release-tag.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("release-tag.sh %v: %v", args, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, value, _ := strings.Cut(line, "=")
		switch name {
		case "tag":
			tag = value
		case "build":
			build = value
		case "publish":
			publish = value
		}
	}
	return tag, build, publish
}

func TestReleaseDecision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the script is for the release runner, which is Linux")
	}
	for _, need := range []string{"bash", "git"} {
		if _, err := exec.LookPath(need); err != nil {
			t.Skipf("%s is not installed", need)
		}
	}

	for _, tc := range []struct {
		name      string
		version   string
		changelog []string
		tags      []string
		args      []string
		tag       string
		build     string
		publish   string
	}{
		{"a release reaches main", "1.9.2", []string{"1.9.2", "1.9.1"}, []string{"v1.9.1"},
			[]string{"main"}, "v1.9.2", "true", "true"},
		{"main is pushed again after its release", "1.9.2", []string{"1.9.2"}, []string{"v1.9.1", "v1.9.2"},
			[]string{"main"}, "v1.9.2", "false", "false"},
		// A version bumped with nothing written for it is not a release.
		{"the version has no notes", "1.9.2", []string{"1.9.1"}, []string{"v1.9.1"},
			[]string{"main"}, "v1.9.2", "false", "false"},
		// 1.9.2 must not pass for 1.9.20's section, or the reverse.
		{"another version's notes are not this one's", "1.9.2", []string{"1.9.20"}, nil,
			[]string{"main"}, "v1.9.2", "false", "false"},
		{"a tag pushed by hand", "1.9.2", []string{"1.9.2"}, []string{"v1.9.2"},
			[]string{"tag", "v1.9.2"}, "v1.9.2", "true", "true"},
		{"a release candidate pushed by hand", "1.9.2", nil, nil,
			[]string{"tag", "v1.10.0-rc.1"}, "v1.10.0-rc.1", "true", "true"},
		{"started by hand", "1.9.2", nil, nil,
			[]string{"dispatch"}, "v1.9.2", "true", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := releaseRepo(t, tc.version, tc.changelog, tc.tags)
			tag, build, publish := decide(t, dir, tc.args...)
			if tag != tc.tag || build != tc.build || publish != tc.publish {
				t.Errorf("tag=%s build=%s publish=%s, want tag=%s build=%s publish=%s",
					tag, build, publish, tc.tag, tc.build, tc.publish)
			}
		})
	}
}
