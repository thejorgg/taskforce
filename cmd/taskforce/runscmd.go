package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thejorgg/taskforce/internal/daemon"
)

func runsCmd(args []string) error {
	if len(args) > 0 && args[0] == "show" {
		return runsShowCmd(args[1:])
	}
	if len(args) > 0 && args[0] == "list" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("runs", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository path")
	limit := fs.Int("limit", 20, "maximum number of runs to list")
	asJSON := fs.Bool("json", false, "print runs as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	records, err := daemon.ListRuns(absRepo, *limit)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(records)
	}
	if len(records) == 0 {
		fmt.Println("no runs yet · start one with: taskforce run --signal \"...\"")
		return nil
	}
	fmt.Printf("%-18s %-18s %-9s %s\n", "id", "status", "age", "task")
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		title := record.Run.Task.Title
		if title == "" {
			title = firstLine(record.Signal)
		}
		fmt.Printf("%-18s %-18s %-9s %s\n", record.ID, record.Status, age(record.CreatedAt), title)
	}
	return nil
}

func runsShowCmd(args []string) error {
	fs := flag.NewFlagSet("runs show", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: taskforce runs show ID [--repo PATH]")
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	record, ok, err := daemon.ReadRun(absRepo, fs.Arg(0))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("run %s not found", fs.Arg(0))
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(record)
}

func logsCmd(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository path")
	follow := fs.Bool("follow", false, "keep printing output until the run finishes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: taskforce logs ID [--follow] [--repo PATH]")
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	id := fs.Arg(0)
	printed := 0
	for {
		events, err := daemon.ReadRunEvents(absRepo, id)
		if err != nil {
			return err
		}
		for _, event := range events[printed:] {
			text := strings.TrimRight(event.Text, "\n")
			for _, line := range strings.Split(text, "\n") {
				fmt.Printf("%s %-14s %s\n", event.CreatedAt.Format("15:04:05"), event.Command, line)
			}
		}
		printed = len(events)
		record, ok, err := daemon.ReadRun(absRepo, id)
		if err != nil {
			return err
		}
		if !ok && printed == 0 && !*follow {
			return fmt.Errorf("run %s not found", id)
		}
		if !*follow {
			return nil
		}
		if ok && !record.Status.Active() {
			fmt.Printf("run %s finished: %s\n", record.ID, record.Status)
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func decideCmd(args []string, approve bool) error {
	name := "deny"
	if approve {
		name = "approve"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository path")
	reason := fs.String("reason", "", "decision note recorded with the run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: taskforce %s ID [--reason \"...\"] [--repo PATH]", name)
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	id := fs.Arg(0)
	if approve {
		if err := daemon.Approve(absRepo, id, *reason); err != nil {
			return err
		}
		fmt.Printf("run %s approved\n", id)
		return nil
	}
	if err := daemon.Deny(absRepo, id, *reason); err != nil {
		return err
	}
	fmt.Printf("run %s denied\n", id)
	return nil
}

func age(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
