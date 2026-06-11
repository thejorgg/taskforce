package main

import (
	"bytes"
	"flag"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/thejorgg/taskforce/internal/merge"
)

func mergeCmd(args []string) error {
	fs := flag.NewFlagSet("merge-all", flag.ContinueOnError)
	target := fs.String("target", "", "target branch to merge into (defaults to current branch)")
	repo := fs.String("repo", ".", "repository path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}

	if *target == "" {
		*target, err = getCurrentBranch(absRepo)
		if err != nil {
			return fmt.Errorf("cannot determine current branch: %w", err)
		}
	}

	fmt.Printf("Merging all worktree branches into %q...\n\n", *target)

	results, mergedBranches, err := merge.MergeAll(absRepo, *target)
	if err != nil {
		return err
	}

	if len(mergedBranches) > 0 {
		if storeErr := merge.StoreRecentMerges(mergedBranches); storeErr != nil {
			fmt.Printf("Warning: could not save recent merges: %v\n", storeErr)
		}
	}

	fmt.Println("\nMerge results:")
	for _, r := range results {
		status := "OK"
		if r.Conflict {
			status = "CONFLICT (resolved)"
		}
		if r.Skipped {
			status = "SKIPPED"
		}
		if r.Error != nil {
			status = fmt.Sprintf("ERROR: %v", r.Error)
		}
		fmt.Printf("  %s: %s\n", r.Branch, status)
	}

	return nil
}

func getCurrentBranch(repo string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(out)), nil
}
