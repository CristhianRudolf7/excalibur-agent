package api

import "time"

// SkillsListParams selects the effective catalog after root precedence.
type SkillsListParams struct {
	Group  string `json:"group,omitempty"`
	Text   string `json:"text,omitempty"`
	Source string `json:"source,omitempty"`
}

type SkillSummary struct {
	Name        string   `json:"name"`
	Group       string   `json:"group"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Source      string   `json:"source"`
	State       string   `json:"state"`
	Editable    bool     `json:"editable"`
	Deletable   bool     `json:"deletable"`
	Tools       []string `json:"tools"`
}

type SkillDetail struct {
	SkillSummary
	Content     string   `json:"content"`
	PathKind    string   `json:"path_kind"`
	Diagnostics []string `json:"diagnostics"`
}

type SkillGroup struct {
	ID            string `json:"id"`
	NameEN        string `json:"name_en"`
	NameES        string `json:"name_es"`
	DescriptionEN string `json:"description_en"`
	DescriptionES string `json:"description_es"`
	Count         int    `json:"count"`
}

type SkillNameParams struct {
	Name string `json:"name"`
}
type SkillsUpdateParams struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}
type SkillsSetStateParams struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type UsageSummaryParams struct {
	Since  *time.Time `json:"since,omitempty"`
	Until  *time.Time `json:"until,omitempty"`
	Bucket string     `json:"bucket,omitempty"`
}

type UsageCounters struct {
	InputTokens  int64   `json:"input_tokens"`
	CachedTokens int64   `json:"cached_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	ToolCalls    int64   `json:"tool_calls"`
}
type UsageTotals struct {
	UsageCounters
	Runs int64 `json:"runs"`
}
type UsageBucket struct {
	Start time.Time `json:"start"`
	UsageCounters
}
type UsageModel struct {
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}
type UsageTool struct {
	Tool     string `json:"tool"`
	Calls    int64  `json:"calls"`
	Failures int64  `json:"failures"`
}
type UsageRun struct {
	RunID         string    `json:"run_id"`
	TaskShortCode string    `json:"task_short_code,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	DurationMS    int64     `json:"duration_ms"`
	InputTokens   int64     `json:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens"`
	CostUSD       float64   `json:"cost_usd"`
	ToolCalls     int64     `json:"tool_calls"`
	Status        string    `json:"status"`
}
type UsageSummaryResult struct {
	Totals     UsageTotals   `json:"totals"`
	ByBucket   []UsageBucket `json:"by_bucket"`
	ByModel    []UsageModel  `json:"by_model"`
	ByTool     []UsageTool   `json:"by_tool"`
	RecentRuns []UsageRun    `json:"recent_runs"`
}
