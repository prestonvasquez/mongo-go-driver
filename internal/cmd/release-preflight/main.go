// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// release-preflight checks that a release can run before the release workflow
// creates or pushes anything. Its flags mirror the Release workflow inputs.
//
// It catches two failures seen in past releases:
//
//   - The release's merge into the next branch needs that branch and full
//     history in the checkout. A shallow, single-branch checkout made 2.9.1
//     and 2.9.2 fail after their tags were already pushed.
//   - The release merges into the next branch with the "ours" strategy, which
//     discards any release-branch change that was not merged up first. That
//     dropped GODRIVER-4075 and GODRIVER-4088 from master after 2.8.x.
//
// Run locally after fetching tags:
//
//	go run ./internal/cmd/release-preflight -remote upstream \
//	  -release-branch release/2.9 -version 2.9.3 -prev-version 2.9.2
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var (
	errNextBranchMissing = errors.New("next branch is not in the checkout")
	errShallowCheckout   = errors.New("checkout is shallow")
	errTagExists         = errors.New("release tag already exists")
	errPrevTagMissing    = errors.New("previous release tag is not on the release branch")
	errVersionMismatch   = errors.New("release branch version does not match the previous version")
	errUnmergedCommits   = errors.New("release branch has commits that are not merged up")
)

const versionFile = "version/version.go"

var driverVersion = regexp.MustCompile(`Driver = "([^"]+)"`)

type config struct {
	repo          string
	remote        string
	releaseBranch string
	nextBranch    string
	version       string
	prevVersion   string

	// skipMergeUp skips the checks for the merge into the next branch, for
	// release branches the release does not merge up.
	skipMergeUp bool
}

func main() {
	releaseBranch := flag.String("release-branch", "", "branch being released, e.g. release/2.9 (required)")
	version := flag.String("version", "", "new version to release, e.g. 2.9.3 (required)")
	prevVersion := flag.String("prev-version", "", "previous tagged version, e.g. 2.9.2 (required)")
	nextBranch := flag.String("next-branch", "master", "branch the release is merged up into")
	remote := flag.String("remote", "origin", "remote to read branches from")
	skipMergeUp := flag.Bool("skip-merge-up", false, "skip merge-up checks, for branches the release does not merge up")
	flag.Parse()

	if *releaseBranch == "" || *version == "" || *prevVersion == "" {
		fmt.Fprintln(os.Stderr, "-release-branch, -version, and -prev-version are required")
		flag.Usage()
		os.Exit(2)
	}

	cfg := config{
		repo:          ".",
		remote:        *remote,
		releaseBranch: *releaseBranch,
		nextBranch:    *nextBranch,
		version:       *version,
		prevVersion:   *prevVersion,
		skipMergeUp:   *skipMergeUp,
	}
	if err := preflight(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "release preflight failed:", err)
		os.Exit(1)
	}

	fmt.Println("release preflight passed")
}

// preflight runs every check against the checkout in cfg.repo and returns the
// first failure.
func preflight(cfg config) error {
	release := cfg.remote + "/" + cfg.releaseBranch
	next := cfg.remote + "/" + cfg.nextBranch

	if !cfg.skipMergeUp {
		if !refExists(cfg.repo, "refs/remotes/"+next) {
			return fmt.Errorf("%w: %s; check out with fetch-depth: 0", errNextBranchMissing, next)
		}

		shallow, err := git(cfg.repo, "rev-parse", "--is-shallow-repository")
		if err != nil {
			return err
		}
		if shallow == "true" {
			return fmt.Errorf("%w; check out with fetch-depth: 0", errShallowCheckout)
		}
	}

	tag := "v" + cfg.version
	if refExists(cfg.repo, "refs/tags/"+tag) {
		return fmt.Errorf("%w: %s; was a previous release run only partly completed?", errTagExists, tag)
	}

	prevTag := "refs/tags/v" + cfg.prevVersion
	if !refExists(cfg.repo, prevTag) || !isAncestor(cfg.repo, prevTag, release) {
		return fmt.Errorf("%w: v%s on %s", errPrevTagMissing, cfg.prevVersion, release)
	}

	src, err := git(cfg.repo, "show", release+":"+versionFile)
	if err != nil {
		return err
	}
	m := driverVersion.FindStringSubmatch(src)
	if m == nil {
		return fmt.Errorf("no Driver version in %s on %s", versionFile, release)
	}
	if m[1] != cfg.prevVersion {
		return fmt.Errorf("%w: %s on %s is %q, want %q", errVersionMismatch, versionFile, release, m[1], cfg.prevVersion)
	}

	if cfg.skipMergeUp {
		return nil
	}

	unmerged, err := git(cfg.repo, "log", "--oneline", "--no-merges", next+".."+release)
	if err != nil {
		return err
	}
	if unmerged != "" {
		return fmt.Errorf("%w into %s; merge up first, or confirm the content is already on %s if the merge-up was squashed:\n%s",
			errUnmergedCommits, next, next, unmerged)
	}

	return nil
}

// git runs a git command in dir and returns its trimmed standard output.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exitErr.Stderr)))
		}

		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(out)), nil
}

func refExists(dir, ref string) bool {
	_, err := git(dir, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

func isAncestor(dir, ancestor, ref string) bool {
	_, err := git(dir, "merge-base", "--is-ancestor", ancestor, ref)
	return err == nil
}
