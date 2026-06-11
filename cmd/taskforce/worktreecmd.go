package main

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/thejorgg/taskforce/internal/worktree"
)

func worktreeCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: taskforce worktree add|list|remove [--repo PATH]")
	}

	action := args[0]
	fs := flag.NewFlagSet("worktree "+action, flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}

	switch action {
	case "add":
		return worktreeAdd(absRepo, fs.Args())
	case "list":
		return worktreeList(absRepo)
	case "remove":
		return worktreeRemove(absRepo, fs.Args())
	default:
		return fmt.Errorf("unknown worktree command %q", action)
	}
}

func worktreeAdd(repo string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: taskforce worktree add <branch>")
	}
	branch := args[0]
	if err := worktree.Add(repo, branch); err != nil {
		return err
	}
	fmt.Printf("worktree created for branch %q at %s\n", branch, worktree.GitWorktreePath(branch))
	return nil
}

func worktreeList(repo string) error {
	wts, err := worktree.List(repo)
	if err != nil {
		return err
	}
	fmt.Print(worktree.FormatList(wts))
	return nil
}

func worktreeRemove(repo string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: taskforce worktree remove <branch>")
	}
	branch := args[0]
	if err := worktree.Remove(repo, branch); err != nil {
		return err
	}
	fmt.Printf("worktree for branch %q removed\n", branch)
	return nil
}
