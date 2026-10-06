// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.mongodb.org/mongo-driver/v2/internal/assert"
	"go.mongodb.org/mongo-driver/v2/internal/require"
)

const (
	releaseBranch = "release/2.9"
	nextBranch    = "master"
)

// A shallow, single-branch checkout is what secure-checkout produces by
// default. The release's "ours" merge cannot check out the next branch from
// it, which is how 2.9.1 and 2.9.2 failed after pushing their tags.
func TestPreflightFailsOnShallowCheckout(t *testing.T) {
	origin := newOrigin(t)

	work := cloneShallow(t, origin, releaseBranch)

	err := preflight(configFor(work, "2.9.2", "2.9.1"))
	assert.ErrorIs(t, err, errNextBranchMissing)
}

// With full history and every release fix already merged up, the release
// can proceed.
func TestPreflightPassesWhenMergedUp(t *testing.T) {
	origin := newOrigin(t)
	commitOn(t, origin, releaseBranch, "fix.go", "fix")
	mergeInto(t, origin, releaseBranch, nextBranch)

	work := cloneFull(t, origin, releaseBranch)

	assert.NoError(t, preflight(configFor(work, "2.9.2", "2.9.1")))
}

// A fix that was never merged up would be discarded by the release's "ours"
// merge, as GODRIVER-4075 and GODRIVER-4088 were after 2.8.x.
func TestPreflightFailsOnUnmergedFix(t *testing.T) {
	origin := newOrigin(t)
	commitOn(t, origin, releaseBranch, "fix.go", "fix")

	work := cloneFull(t, origin, releaseBranch)

	err := preflight(configFor(work, "2.9.2", "2.9.1"))
	assert.ErrorIs(t, err, errUnmergedCommits)
	assert.ErrorContains(t, err, "add fix.go")
}

// Re-running a release whose tag was already pushed, as 2.9.2 was, must stop
// before the release workflow tries to create the tag again.
func TestPreflightFailsWhenTagAlreadyExists(t *testing.T) {
	origin := newOrigin(t)
	tagOn(t, origin, releaseBranch, "v2.9.2")

	work := cloneFull(t, origin, releaseBranch)

	err := preflight(configFor(work, "2.9.2", "2.9.1"))
	assert.ErrorIs(t, err, errTagExists)
}

// A tag pushed without the branch's version bump, the state 2.9.1 and 2.9.2
// were left in, must be caught before the next release.
func TestPreflightFailsWhenBranchNotAtPrevVersion(t *testing.T) {
	origin := newOrigin(t)
	tagOn(t, origin, releaseBranch, "v2.9.2")

	work := cloneFull(t, origin, releaseBranch)

	err := preflight(configFor(work, "2.9.3", "2.9.2"))
	assert.ErrorIs(t, err, errVersionMismatch)
}

// A typo in the previous version input must not reach the release.
func TestPreflightFailsWhenPrevTagMissing(t *testing.T) {
	origin := newOrigin(t)

	work := cloneFull(t, origin, releaseBranch)

	err := preflight(configFor(work, "2.9.2", "2.9.0"))
	assert.ErrorIs(t, err, errPrevTagMissing)
}

// Branches the release does not merge up, such as release/1.17, may carry
// commits the next branch never gets, and may be checked out shallow. The
// version and tag checks still apply.
func TestPreflightSkipMergeUp(t *testing.T) {
	origin := newOrigin(t)
	commitOn(t, origin, releaseBranch, "fix.go", "fix")

	work := cloneShallow(t, origin, releaseBranch)
	run(t, work, "fetch", "--quiet", "--tags", "--unshallow")

	cfg := configFor(work, "2.9.2", "2.9.1")
	cfg.skipMergeUp = true
	assert.NoError(t, preflight(cfg))

	cfg.prevVersion = "2.9.0"
	assert.ErrorIs(t, preflight(cfg), errPrevTagMissing)
}

func configFor(repo, version, prevVersion string) config {
	return config{
		repo:          repo,
		remote:        "origin",
		releaseBranch: releaseBranch,
		nextBranch:    nextBranch,
		version:       version,
		prevVersion:   prevVersion,
	}
}

// newOrigin returns a bare repository with a master branch and a release/2.9
// branch cut from it at version 2.9.1, tagged v2.9.1.
func newOrigin(t *testing.T) string {
	t.Helper()

	seed := filepath.Join(t.TempDir(), "seed")
	run(t, "", "init", "--quiet", "--initial-branch", nextBranch, seed)
	writeVersion(t, seed, "2.9.1")
	run(t, seed, "add", ".")
	run(t, seed, "commit", "--quiet", "-m", "initial")
	run(t, seed, "branch", releaseBranch)
	run(t, seed, "tag", "v2.9.1", releaseBranch)

	origin := filepath.Join(t.TempDir(), "origin.git")
	run(t, "", "clone", "--quiet", "--bare", seed, origin)

	return origin
}

// commitOn adds a commit that creates file on branch in origin.
func commitOn(t *testing.T, origin, branch, file, content string) {
	t.Helper()

	work := cloneFull(t, origin, branch)
	require.NoError(t, os.WriteFile(filepath.Join(work, file), []byte(content), 0o600))
	run(t, work, "add", file)
	run(t, work, "commit", "--quiet", "-m", "add "+file)
	run(t, work, "push", "--quiet", "origin", branch)
}

// mergeInto merges branch into target in origin, as a merge-up PR would.
func mergeInto(t *testing.T, origin, branch, target string) {
	t.Helper()

	work := cloneFull(t, origin, target)
	run(t, work, "merge", "--quiet", "--no-edit", "origin/"+branch)
	run(t, work, "push", "--quiet", "origin", target)
}

// tagOn tags the tip of branch in origin.
func tagOn(t *testing.T, origin, branch, tag string) {
	t.Helper()

	work := cloneFull(t, origin, branch)
	run(t, work, "tag", tag)
	run(t, work, "push", "--quiet", "origin", tag)
}

// cloneShallow clones only branch at depth 1, like secure-checkout's default.
func cloneShallow(t *testing.T, origin, branch string) string {
	t.Helper()

	work := filepath.Join(t.TempDir(), "work")
	run(t, "", "clone", "--quiet", "--depth", "1", "--branch", branch, "file://"+origin, work)

	return work
}

// cloneFull clones every branch and tag, like secure-checkout with
// fetch-depth: 0.
func cloneFull(t *testing.T, origin, branch string) string {
	t.Helper()

	work := filepath.Join(t.TempDir(), "work")
	run(t, "", "clone", "--quiet", "--branch", branch, origin, work)

	return work
}

func writeVersion(t *testing.T, dir, version string) {
	t.Helper()

	path := filepath.Join(dir, versionFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	src := fmt.Sprintf("package version\n\n// Driver is the current version of the driver.\nvar Driver = %q\n", version)
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))
}

// run runs git in dir with a fixed identity and fails the test on error.
func run(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}
