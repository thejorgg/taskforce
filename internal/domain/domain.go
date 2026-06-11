package domain

import "time"

type StageName string

const (
	StageEcho     StageName = "Echo"
	StageDispatch StageName = "Dispatch"
	StageRelay    StageName = "Relay"
	StageScope    StageName = "Scope"
	StageExfil    StageName = "Exfil"
)

type StageStatus string

const (
	StatusIdle          StageStatus = "idle"
	StatusRunning       StageStatus = "running"
	StatusPassed        StageStatus = "passed"
	StatusFailed        StageStatus = "failed"
	StatusSkipped       StageStatus = "skipped"
	StatusNeedsRevision StageStatus = "needs_revision"
)

type Signal struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Content   string    `json:"content"`
	Artifacts []string  `json:"artifacts,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskPacket struct {
	ID                 string      `json:"id"`
	Title              string      `json:"title"`
	Description        string      `json:"description"`
	Source             string      `json:"source"`
	Severity           string      `json:"severity"`
	Priority           int         `json:"priority"`
	Category           string      `json:"category"`
	RelevantArtifacts  []string    `json:"relevant_artifacts,omitempty"`
	AcceptanceCriteria []string    `json:"acceptance_criteria,omitempty"`
	Status             string      `json:"status"`
	Actionable         bool        `json:"actionable"`
	Signal             Signal      `json:"signal"`
	CreatedAt          time.Time   `json:"created_at"`
	Metadata           StringTable `json:"metadata,omitempty"`
}

type StringTable map[string]string

type CommandSpec struct {
	Name     string            `json:"name"`
	Run      string            `json:"run,omitempty"`
	Argv     []string          `json:"argv,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	WorkDir  string            `json:"work_dir,omitempty"`
	Timeout  string            `json:"timeout,omitempty"`
	Required bool              `json:"required"`
	Mutates  bool              `json:"mutates"`
}

type CommandResult struct {
	Name      string        `json:"name"`
	Command   string        `json:"command"`
	ExitCode  int           `json:"exit_code"`
	Stdout    string        `json:"stdout,omitempty"`
	Stderr    string        `json:"stderr,omitempty"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at"`
	Duration  time.Duration `json:"duration"`
	Skipped   bool          `json:"skipped"`
	Error     string        `json:"error,omitempty"`
}

func (r CommandResult) Output() string {
	if r.Stdout == "" {
		return r.Stderr
	}
	if r.Stderr == "" {
		return r.Stdout
	}
	return r.Stdout + "\n" + r.Stderr
}

type ExecutionPlan struct {
	Summary string         `json:"summary"`
	Steps   []string       `json:"steps"`
	Command *CommandSpec   `json:"command,omitempty"`
	Result  *CommandResult `json:"result,omitempty"`
	Meta    StringTable    `json:"meta,omitempty"`
	Task    TaskPacket     `json:"task"`
	Created time.Time      `json:"created"`
}

type RelayResult struct {
	Plan        ExecutionPlan  `json:"plan"`
	BuildResult *CommandResult `json:"build_result,omitempty"`
	Attempts    int            `json:"attempts"`
	Approved    bool           `json:"approved"`
	Feedback    string         `json:"feedback,omitempty"`
}

type ReviewStatus string

const (
	ReviewApproved      ReviewStatus = "approved"
	ReviewRejected      ReviewStatus = "rejected"
	ReviewNeedsRevision ReviewStatus = "needs_revision"
)

type ReviewResult struct {
	Status   ReviewStatus    `json:"status"`
	Reason   string          `json:"reason"`
	Hooks    []CommandResult `json:"hooks,omitempty"`
	Feedback []string        `json:"feedback,omitempty"`
}

type ReleaseResult struct {
	Skipped bool            `json:"skipped"`
	Branch  string          `json:"branch,omitempty"`
	Commit  string          `json:"commit,omitempty"`
	Pushed  bool            `json:"pushed"`
	PRURL   string          `json:"pr_url,omitempty"`
	Results []CommandResult `json:"results,omitempty"`
}

type StageSnapshot struct {
	Name       StageName   `json:"name"`
	Status     StageStatus `json:"status"`
	Logs       []string    `json:"logs,omitempty"`
	LogEntries []StageLog  `json:"log_entries,omitempty"`
}

type StageLog struct {
	CreatedAt time.Time `json:"created_at"`
	Text      string    `json:"text"`
}

type PipelineRun struct {
	ID        string          `json:"id"`
	Repo      string          `json:"repo"`
	Signal    Signal          `json:"signal"`
	Task      TaskPacket      `json:"task"`
	Relay     RelayResult     `json:"relay"`
	Review    ReviewResult    `json:"review"`
	Release   ReleaseResult   `json:"release"`
	Stages    []StageSnapshot `json:"stages"`
	StartedAt time.Time       `json:"started_at"`
	EndedAt   time.Time       `json:"ended_at"`
}
