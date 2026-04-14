package devcleanup

import (
	"context"
	"io"
	"time"
)

type RiskLevel int

const (
	RiskSafe RiskLevel = iota + 1
	RiskModerate
	RiskAggressive
)

func ParseRiskLevel(raw string) RiskLevel {
	switch raw {
	case "safe":
		return RiskSafe
	case "moderate":
		return RiskModerate
	case "aggressive":
		return RiskAggressive
	default:
		return RiskSafe
	}
}

func (r RiskLevel) String() string {
	switch r {
	case RiskSafe:
		return "safe"
	case RiskModerate:
		return "moderate"
	case RiskAggressive:
		return "aggressive"
	default:
		return "unknown"
	}
}

type TaskKind string

const (
	TaskKindPath    TaskKind = "path"
	TaskKindCommand TaskKind = "command"
	TaskKindPattern TaskKind = "pattern"
)

type PathTask struct {
	Path            string
	RemoveDirectory bool
	MinAge          time.Duration
}

type CommandTask struct {
	Executable string
	Args       []string
}

type CleanupTask struct {
	ID           string
	Kind         TaskKind
	Name         string
	Description  string
	Category     string
	Risk         RiskLevel
	ProcessHints []string
	PathTask     *PathTask
	CommandTask  *CommandTask
	PatternTask  *PatternTask
}

type PatternTask struct {
	Roots          []string
	DirectoryNames []string
	MinAge         time.Duration
}

type Provider interface {
	ID() string
	Tasks(env Environment) []CleanupTask
}

type Environment struct {
	OS      string
	HomeDir string
	TempDir string
}

type Config struct {
	MaxRisk             RiskLevel
	DryRun              bool
	AssumeYes           bool
	Verbose             bool
	DisableCommandTasks bool
	Parallelism         int
	MinAge              time.Duration
	ProcessAware        bool
	IncludeCategories   map[string]struct{}
	ExcludeIDs          map[string]struct{}
	IncludeIDs          map[string]struct{}
	PathOverrides       map[string][]string
	PatternRoots        map[string][]string
}

type Prompt interface {
	Confirm(message string) bool
}

type Logger struct {
	Out     io.Writer
	Verbose bool
}

type PlanItem struct {
	Task          CleanupTask
	Exists        bool
	EstimatedSize int64
	SkippedReason string
	Err           error
}

type ExecutionResult struct {
	Task         CleanupTask
	Attempted    bool
	DeletedBytes int64
	DeletedItems int
	Skipped      bool
	Err          error
	Duration     time.Duration
}

type RunReport struct {
	GeneratedAt    time.Time           `json:"generated_at"`
	OS             string              `json:"os"`
	DryRun         bool                `json:"dry_run"`
	MaxRisk        string              `json:"max_risk"`
	Parallelism    int                 `json:"parallelism"`
	Planned        int                 `json:"planned"`
	Skipped        int                 `json:"skipped"`
	Attempted      int                 `json:"attempted"`
	ReclaimedBytes int64               `json:"reclaimed_bytes"`
	FreedByVolume  map[string]int64    `json:"freed_by_volume,omitempty"`
	Duration       time.Duration       `json:"duration"`
	Results        []ResultReportEntry `json:"results"`
}

type ResultReportEntry struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Risk         string `json:"risk"`
	Attempted    bool   `json:"attempted"`
	DeletedBytes int64  `json:"deleted_bytes"`
	DeletedItems int    `json:"deleted_items"`
	Skipped      bool   `json:"skipped"`
	Error        string `json:"error,omitempty"`
}

type Engine struct {
	providers []Provider
	logger    Logger
	prompt    Prompt
}

func NewEngine(providers []Provider, logger Logger, prompt Prompt) *Engine {
	return &Engine{providers: providers, logger: logger, prompt: prompt}
}

type Runner interface {
	Run(ctx context.Context, cfg Config) (RunReport, error)
}
