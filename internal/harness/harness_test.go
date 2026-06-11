package harness

import (
	"strings"
	"testing"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
)

func task() domain.TaskPacket {
	return domain.TaskPacket{ID: "task-123", Title: "Fix login", Description: "The login button is broken."}
}

func TestStageOverrideWinsOverAgent(t *testing.T) {
	spec, err := Resolve(Request{
		Stage:  "relay.build",
		Mode:   ModeBuild,
		Prompt: "do it",
		Task:   task(),
		Config: config.StageConfig{Agent: "codex", Argv: []string{"mytool", "--task", "{{task_id}}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Argv[0] != "mytool" {
		t.Fatalf("expected stage argv override, got %v", spec.Argv)
	}
	if spec.Argv[2] != "task-123" {
		t.Fatalf("expected task_id placeholder expansion, got %v", spec.Argv)
	}
	if !spec.Mutates {
		t.Fatal("build mode must mark the command as mutating")
	}
}

func TestCustomAgentResolution(t *testing.T) {
	agents := map[string]config.AgentConfig{
		"aider": {
			Argv:    []string{"aider", "--message", "{{prompt}}"},
			Build:   &config.AgentCommand{Argv: []string{"aider", "--yes", "--message", "{{prompt}}"}},
			Env:     map[string]string{"AIDER_FLAG": "1"},
			Timeout: "10m",
		},
	}
	plan, err := Resolve(Request{Stage: "relay.control", Mode: ModePlan, Prompt: "plan it", Task: task(), Config: config.StageConfig{Agent: "aider"}, Agents: agents})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Argv[1] != "--message" || plan.Argv[2] != "plan it" {
		t.Fatalf("plan mode should use default argv with prompt, got %v", plan.Argv)
	}
	if plan.Env["AIDER_FLAG"] != "1" {
		t.Fatal("custom agent env not merged")
	}
	if plan.Timeout != "10m" {
		t.Fatalf("custom agent timeout not applied, got %q", plan.Timeout)
	}
	if plan.Mutates {
		t.Fatal("plan mode must not mark the command as mutating")
	}
	build, err := Resolve(Request{Stage: "relay.build", Mode: ModeBuild, Prompt: "build it", Task: task(), Config: config.StageConfig{Agent: "aider"}, Agents: agents})
	if err != nil {
		t.Fatal(err)
	}
	if build.Argv[1] != "--yes" {
		t.Fatalf("build mode should use the build variant, got %v", build.Argv)
	}
}

func TestBuiltinAdapters(t *testing.T) {
	for _, name := range BuiltinNames() {
		for _, mode := range []Mode{ModePlan, ModeBuild} {
			spec, err := Resolve(Request{Stage: "relay.test", Mode: mode, Prompt: "p", Task: task(), Config: config.StageConfig{Agent: name, Model: "m1"}})
			if err != nil {
				t.Fatalf("builtin %s/%s: %v", name, mode, err)
			}
			if len(spec.Argv) == 0 {
				t.Fatalf("builtin %s/%s produced empty argv", name, mode)
			}
			joined := strings.Join(spec.Argv, " ")
			if !strings.Contains(joined, "m1") {
				t.Fatalf("builtin %s/%s did not pass model: %v", name, mode, spec.Argv)
			}
			if !strings.Contains(joined, "p") {
				t.Fatalf("builtin %s/%s did not pass prompt: %v", name, mode, spec.Argv)
			}
		}
	}
}

func TestCodexSandboxPerMode(t *testing.T) {
	plan, _ := Resolve(Request{Stage: "s", Mode: ModePlan, Prompt: "p", Task: task(), Config: config.StageConfig{Agent: "codex"}})
	if !contains(plan.Argv, "read-only") {
		t.Fatalf("codex plan must be read-only sandbox: %v", plan.Argv)
	}
	build, _ := Resolve(Request{Stage: "s", Mode: ModeBuild, Prompt: "p", Task: task(), Config: config.StageConfig{Agent: "codex"}})
	if !contains(build.Argv, "workspace-write") {
		t.Fatalf("codex build must be workspace-write sandbox: %v", build.Argv)
	}
}

func TestMimoBuiltinUsesRunAndNeverAsk(t *testing.T) {
	spec, err := Resolve(Request{Stage: "relay.build", Mode: ModeBuild, Prompt: "fix it", Task: task(), Config: config.StageConfig{Agent: "mimo", Model: "mimo/auto"}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Argv, " ")
	for _, want := range []string{"mimo run", "--dangerously-skip-permissions", "--model mimo/auto", "fix it"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("mimo argv missing %q: %v", want, spec.Argv)
		}
	}
}

func TestDefaultAgents(t *testing.T) {
	plan, err := Resolve(Request{Stage: "s", Mode: ModePlan, Prompt: "p", Task: task(), Config: config.StageConfig{}})
	if err != nil || plan.Argv[0] != "codex" {
		t.Fatalf("empty agent in plan mode should default to codex, got %v err %v", plan.Argv, err)
	}
	build, err := Resolve(Request{Stage: "s", Mode: ModeBuild, Prompt: "p", Task: task(), Config: config.StageConfig{}})
	if err != nil || build.Argv[0] != "opencode" {
		t.Fatalf("empty agent in build mode should default to opencode, got %v err %v", build.Argv, err)
	}
}

func TestUnknownAgentErrors(t *testing.T) {
	_, err := Resolve(Request{Stage: "relay.build", Mode: ModeBuild, Prompt: "p", Task: task(), Config: config.StageConfig{Agent: "nonexistent"}})
	if err == nil {
		t.Fatal("unknown agent must return an error")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("error should name the agent: %v", err)
	}
}

func TestStageEnvCarriesTaskContext(t *testing.T) {
	spec, err := Resolve(Request{Stage: "relay.build", Mode: ModeBuild, Prompt: "the prompt", Task: task(), Config: config.StageConfig{Agent: "opencode"}, Repo: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"TASKFORCE_TASK_ID": "task-123",
		"TASKFORCE_STAGE":   "relay.build",
		"TASKFORCE_MODE":    "build",
		"TASKFORCE_PROMPT":  "the prompt",
		"TASKFORCE_REPO":    "/repo",
	} {
		if spec.Env[key] != want {
			t.Fatalf("env %s = %q, want %q", key, spec.Env[key], want)
		}
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func TestBuiltinNamesMatchConfigValidation(t *testing.T) {
	cfg := config.Default()
	for _, name := range BuiltinNames() {
		cfg.Relay.Control.Agent = name
		if err := config.Validate(cfg); err != nil {
			t.Fatalf("builtin %q rejected by config.Validate: %v", name, err)
		}
	}
}

func TestEnvAndRunPlaceholdersExpand(t *testing.T) {
	spec, err := Resolve(Request{
		Stage: "relay.build",
		Mode:  ModeBuild,
		Task:  domain.TaskPacket{ID: "task-9"},
		Repo:  "/work/repo",
		Config: config.StageConfig{
			Run: "mytool --task {{task_id}}",
			Env: map[string]string{"MYTOOL_REPO": "{{repo}}"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Run != "mytool --task task-9" {
		t.Fatalf("run = %q", spec.Run)
	}
	if spec.Env["MYTOOL_REPO"] != "/work/repo" {
		t.Fatalf("env = %q", spec.Env["MYTOOL_REPO"])
	}
}

func TestStageWorkDirPropagatesAndExpands(t *testing.T) {
	spec, err := Resolve(Request{
		Stage: "relay.build",
		Mode:  ModeBuild,
		Task:  task(),
		Repo:  "/work/repo",
		Config: config.StageConfig{
			Run:     "mytool",
			WorkDir: "{{repo}}/api",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.WorkDir != "/work/repo/api" {
		t.Fatalf("work_dir = %q", spec.WorkDir)
	}
}
