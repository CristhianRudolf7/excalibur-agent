package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// ToolRetrievalQuery is one deterministic fixture query with its
// expected tool (Section 23.5 tool-retrieval).
type ToolRetrievalQuery struct {
	Query    string `json:"query"`
	Expected string `json:"expected"`
}

// DefaultToolRetrievalQueries pins the retrieval corpus: each query
// names the verb, object and domain its target description is written
// for (Section 13.3: descriptions are written to be found).
func DefaultToolRetrievalQueries() []ToolRetrievalQuery {
	return []ToolRetrievalQuery{
		{Query: "convert docx pdf document", Expected: "file_convert"},
		{Query: "read pdf document text anchors", Expected: "document_read"},
		{Query: "merge split pdf pages", Expected: "pdf_ops"},
		{Query: "render mermaid diagram svg", Expected: "diagram_render"},
		{Query: "chart bar data plot", Expected: "chart_render"},
		{Query: "extract vision image text layout", Expected: "vision_extract"},
		{Query: "search github source code", Expected: "source_search"},
		{Query: "create tool package custom", Expected: "tool_package"},
	}
}

// ToolRetrievalReport scores search_tools precision.
type ToolRetrievalReport struct {
	Queries      int     `json:"queries"`
	PrecisionAt1 float64 `json:"precision_at_1"`
	PrecisionAt5 float64 `json:"precision_at_5"`
	MRR          float64 `json:"mrr"`
	At           string  `json:"at"`
}

// PrepareToolRetrieval materializes the deterministic fixture set with
// its hash (Section 23.8 deterministic setup).
func PrepareToolRetrieval(dir string) (string, error) {
	queries := DefaultToolRetrievalQueries()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("eval: mkdir tool-retrieval: %w", err)
	}
	raw, _ := json.MarshalIndent(queries, "", "  ")
	path := filepath.Join(dir, "tool-queries.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("eval: write tool-queries: %w", err)
	}
	hash := ToolRetrievalHash(queries)
	if err := os.WriteFile(filepath.Join(dir, "tool-queries.sha256"), []byte(hash+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("eval: write tool-retrieval hash: %w", err)
	}
	return hash, nil
}

// ToolRetrievalHash deterministically hashes the query corpus.
func ToolRetrievalHash(queries []ToolRetrievalQuery) string {
	cp := append([]ToolRetrievalQuery(nil), queries...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Query < cp[j].Query })
	h := sha256.New()
	for _, q := range cp {
		fmt.Fprintf(h, "%s\t%s\n", q.Query, q.Expected)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RunToolRetrieval scores a deferred catalog: for each fixture query it
// runs the same BM25 search search_tools serves and measures
// precision@1, precision@5 and MRR.
func RunToolRetrieval(cat *tools.DeferredCatalog) ToolRetrievalReport {
	queries := DefaultToolRetrievalQueries()
	return RunToolRetrievalOn(cat, queries)
}

// RunToolRetrievalOn scores explicit queries (tests and harnesses).
func RunToolRetrievalOn(cat *tools.DeferredCatalog, queries []ToolRetrievalQuery) ToolRetrievalReport {
	if len(queries) == 0 || cat == nil {
		return ToolRetrievalReport{At: time.Now().UTC().Format(time.RFC3339)}
	}
	var p1, p5, mrr float64
	for _, q := range queries {
		hits := cat.Search(q.Query, 5)
		rank := 0
		for i, h := range hits {
			if h.Definition.Name == q.Expected {
				rank = i + 1
				break
			}
		}
		if rank == 1 {
			p1++
		}
		if rank >= 1 && rank <= 5 {
			p5++
			mrr += 1 / float64(rank)
		}
	}
	n := float64(len(queries))
	return ToolRetrievalReport{
		Queries: len(queries), PrecisionAt1: p1 / n, PrecisionAt5: p5 / n,
		MRR: mrr / n, At: time.Now().UTC().Format(time.RFC3339),
	}
}

// LoadToolRetrievalQueries reads a prepared fixture file.
func LoadToolRetrievalQueries(dir string) ([]ToolRetrievalQuery, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "tool-queries.json"))
	if err != nil {
		return nil, fmt.Errorf("eval: read tool-queries: %w", err)
	}
	var out []ToolRetrievalQuery
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("eval: parse tool-queries: %w", err)
	}
	return out, nil
}

// TerminalTask is one terminal-vs-tools fixture: a task solvable both
// with the terminal alone and with terminal plus specific tools
// (Section 23.5 terminal-vs-tools).
type TerminalTask struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// NeedsSpecific names the specific tools the tools arm may use.
	NeedsSpecific []string `json:"needs_specific"`
}

// DefaultTerminalTasks pins the comparison corpus.
func DefaultTerminalTasks() []TerminalTask {
	return []TerminalTask{
		{Name: "find-todo", Description: "Find every TODO comment in the repo and list file:line", NeedsSpecific: []string{"grep"}},
		{Name: "rename-symbol", Description: "Rename a symbol across two files and verify no stale references", NeedsSpecific: []string{"edit_file", "grep"}},
		{Name: "read-window", Description: "Read lines 40-80 of a large file and quote them", NeedsSpecific: []string{"read_file"}},
		{Name: "convert-doc", Description: "Convert a Markdown note to HTML and check headings survived", NeedsSpecific: []string{"file_convert"}},
		{Name: "extract-pdf", Description: "Extract the first page of a PDF and check the title", NeedsSpecific: []string{"document_read"}},
	}
}

// TerminalArm is one comparison arm.
type TerminalArm string

// Arms.
const (
	ArmTerminalOnly  TerminalArm = "terminal-only"
	ArmTerminalTools TerminalArm = "terminal-plus-tools"
)

// TerminalTrial is one simulated comparison point: the harness records
// which arm solved the fixture and at what token, time and error cost.
// Real wall-clock measurement plugs in at H8 with the engine; the
// scaffold pins the deterministic accounting shape now.
type TerminalTrial struct {
	Task      string      `json:"task"`
	Arm       TerminalArm `json:"arm"`
	Success   bool        `json:"success"`
	Tokens    int64       `json:"tokens"`
	Millis    int64       `json:"millis"`
	Errors    int         `json:"errors"`
	ToolCalls int64       `json:"tool_calls"`
}

// TerminalReport compares the two arms task by task.
type TerminalReport struct {
	Tasks []string `json:"tasks"`
	// ByTask maps task -> arm -> trial.
	ByTask map[string]map[TerminalArm]TerminalTrial `json:"by_task"`
	// Deltas summarize tools-arm minus terminal-only per task average.
	AvgTokenDelta float64 `json:"avg_token_delta"`
	AvgTimeDelta  float64 `json:"avg_time_delta"`
	SuccessOnly   int     `json:"success_terminal_only"`
	SuccessTools  int     `json:"success_terminal_tools"`
	At            string  `json:"at"`
}

// PrepareTerminalTasks materializes the fixture set with its hash.
func PrepareTerminalTasks(dir string) (string, error) {
	tasks := DefaultTerminalTasks()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("eval: mkdir terminal-vs-tools: %w", err)
	}
	raw, _ := json.MarshalIndent(tasks, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "terminal-tasks.json"), append(raw, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("eval: write terminal-tasks: %w", err)
	}
	hash := TerminalTasksHash(tasks)
	if err := os.WriteFile(filepath.Join(dir, "terminal-tasks.sha256"), []byte(hash+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("eval: write terminal-tasks hash: %w", err)
	}
	return hash, nil
}

// TerminalTasksHash hashes the task corpus.
func TerminalTasksHash(tasks []TerminalTask) string {
	cp := append([]TerminalTask(nil), tasks...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	h := sha256.New()
	for _, t := range cp {
		fmt.Fprintf(h, "%s\t%s\t%s\n", t.Name, t.Description, strings.Join(t.NeedsSpecific, ","))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SimulateTerminalComparison builds the deterministic report shape from
// simulated trials: the terminal-only arm spends more tokens and time
// (shell round-trips) while the tools arm spends fewer with specific
// tools. Callers replace simulated trials with measured ones; the
// aggregation stays identical.
func SimulateTerminalComparison(trials []TerminalTrial) TerminalReport {
	byTask := map[string]map[TerminalArm]TerminalTrial{}
	for _, tr := range trials {
		if byTask[tr.Task] == nil {
			byTask[tr.Task] = map[TerminalArm]TerminalTrial{}
		}
		byTask[tr.Task][tr.Arm] = tr
	}
	var tokDelta, timeDelta float64
	var n float64
	var sOnly, sTools int
	for _, arms := range byTask {
		only, okOnly := arms[ArmTerminalOnly]
		with, okTools := arms[ArmTerminalTools]
		if !okOnly || !okTools {
			continue
		}
		n++
		tokDelta += float64(with.Tokens - only.Tokens)
		timeDelta += float64(with.Millis - only.Millis)
		if only.Success {
			sOnly++
		}
		if with.Success {
			sTools++
		}
	}
	rep := TerminalReport{ByTask: byTask, At: time.Now().UTC().Format(time.RFC3339)}
	for task := range byTask {
		rep.Tasks = append(rep.Tasks, task)
	}
	sort.Strings(rep.Tasks)
	if n > 0 {
		rep.AvgTokenDelta = tokDelta / n
		rep.AvgTimeDelta = timeDelta / n
	}
	rep.SuccessOnly = sOnly
	rep.SuccessTools = sTools
	return rep
}
