package daemon

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
)

// ChoiceDecider is the narrow seam over the Section 19.4 decision
// model both the deterministic table-2.1 ports and the daemon
// adapters use. models.Decider (HTTPDecider, pplx, LLM fallback,
// FakeDecider) satisfies it.
type ChoiceDecider interface {
	Ask(ctx context.Context, q models.Question) (models.Answer, error)
}

// ModelDecider answers master triage through the calibrated decision
// model at the Table 2.1 intake row (Section 19.4): type, scope,
// durable-instruction and clarify questions. Rules decide when they
// suffice (explicit commands and resolved control orders); the model
// is the first option for the rest, and a failing model degrades to
// the deterministic triage instead of blocking intake (6.1 cost
// rule: intake never waits on an LLM).
type ModelDecider struct {
	Dec      ChoiceDecider
	Thresh   models.Thresholds
	Scopes   []string
	AskAfter time.Duration
}

// NewModelDecider returns the intake decider. A nil Dec degrades
// every classification to deterministic triage.
func NewModelDecider(dec ChoiceDecider, scopes []string) *ModelDecider {
	return &ModelDecider{Dec: dec, Thresh: models.DefaultThresholds(), Scopes: scopes}
}

// Classify implements master.Decider.
func (d *ModelDecider) Classify(ctx context.Context, text string, pre master.Preanalysis) master.Triage {
	if d.Dec == nil {
		return deterministicTriage(text, pre)
	}
	scopeOpts := append([]string(nil), d.Scopes...)
	if len(scopeOpts) == 0 {
		scopeOpts = []string{"general"}
	}
	qs := models.TriageQuestions(text, triageContext(pre), scopeOpts)
	typeAns, err := d.Dec.Ask(ctx, qs[0])
	if err != nil {
		return deterministicTriage(text, pre)
	}
	tri := master.Triage{Type: mapTriageType(typeAns.Choice), Confidence: typeAns.Prob}
	if models.Decide(typeAns.Prob, d.Thresh) == models.OutcomeAlternative {
		tri.Type = master.TriageConversation
	}
	scopeAns, err := d.Dec.Ask(ctx, qs[1])
	if err == nil && scopeAns.Choice != "" {
		if models.Decide(scopeAns.Prob, d.Thresh) != models.OutcomeAlternative {
			tri.Scopes = []string{scopeAns.Choice}
		}
	}
	durableAns, err := d.Dec.Ask(ctx, qs[2])
	if err == nil && models.Decide(durableAns.Prob, d.Thresh) == models.OutcomeAct {
		tri.DurableFrag = []string{text}
	}
	clarifyAns, err := d.Dec.Ask(ctx, qs[3])
	clarifyProb := 0.0
	if err == nil {
		clarifyProb = clarifyAns.Prob
	}
	mandatoryMissing := pre.ControlIntent != "" && strings.TrimSpace(pre.ControlTarget) == ""
	irreversible := pre.ControlIntent == "cancel"
	reworkCostly := tri.Type == master.TriageTask || tri.Type == master.TriageCorrection
	tri.NeedsClarify = models.Decide(clarifyProb, d.Thresh) == models.OutcomeAct ||
		models.ShouldAsk(typeAns.Prob, mandatoryMissing, irreversible, reworkCostly)
	if tri.Type == master.TriageCorrection {
		tri.CorrectionTarget = strings.TrimSpace(pre.ControlTarget)
	}
	return tri
}

// mapTriageType maps the decider request-type vocabulary to the
// master triage types.
func mapTriageType(choice string) master.TriageType {
	switch choice {
	case "status_query":
		return master.TriageStatusQuery
	case "quick_action":
		return master.TriageStatusQuery
	case "task":
		return master.TriageTask
	case "correction_or_redirect":
		return master.TriageCorrection
	case "schedule_or_reminder":
		return master.TriageSchedule
	case "memory_instruction":
		return master.TriageMemoryWrite
	case "configuration":
		return master.TriageConfig
	default:
		return master.TriageConversation
	}
}

// triageContext packs the deterministic preanalysis into the minimal
// context the decision model needs (no history, no full state).
func triageContext(pre master.Preanalysis) string {
	var sb strings.Builder
	if pre.IsCommand {
		sb.WriteString("command: " + pre.Command + " " + pre.CommandArgs + "\n")
	}
	if pre.ControlIntent != "" {
		sb.WriteString("control: " + pre.ControlIntent + " target=" + pre.ControlTarget + "\n")
	}
	if len(pre.TaskRefs) > 0 {
		sb.WriteString("refs: " + strings.Join(pre.TaskRefs, ",") + "\n")
	}
	if len(pre.Paths) > 0 {
		sb.WriteString("paths: " + strings.Join(pre.Paths, ",") + "\n")
	}
	if pre.HasMandate {
		sb.WriteString("durable instruction present\n")
	}
	return sb.String()
}

// deterministicTriage mirrors the Table 2.1 "rules when they suffice"
// row: explicit commands and resolved control orders classify without
// any model call.
func deterministicTriage(text string, pre master.Preanalysis) master.Triage {
	lower := strings.ToLower(strings.TrimSpace(text))
	tri := master.Triage{Type: master.TriageConversation, Confidence: 1}
	switch {
	case pre.IsCommand:
		tri.Type = master.TriageStatusQuery
	case pre.ControlIntent == "status":
		tri.Type = master.TriageStatusQuery
	case pre.ControlIntent == "pause" || pre.ControlIntent == "resume" ||
		pre.ControlIntent == "cancel" || pre.ControlIntent == "redirect" ||
		pre.ControlIntent == "stop" || pre.ControlIntent == "approve" ||
		pre.ControlIntent == "deny":
		tri.Type = master.TriageStatusQuery
		tri.CorrectionTarget = strings.TrimSpace(pre.ControlTarget)
		if pre.ControlIntent == "redirect" {
			tri.Type = master.TriageCorrection
		}
		if strings.TrimSpace(pre.ControlTarget) == "" {
			tri.NeedsClarify = true
		}
	case pre.HasMandate || strings.Contains(lower, "remember") || strings.Contains(lower, "remind"):
		tri.Type = master.TriageSchedule
		if pre.HasMandate {
			tri.DurableFrag = []string{text}
		}
	case len(pre.TaskRefs) > 0 || len(pre.Paths) > 0 || len(pre.URLs) > 0:
		tri.Type = master.TriageTask
	}
	return tri
}

// MemoryJudge answers the Table 2.1 memory-admission row through the
// calibrated judge (Sections 10.5, 19.4): duplicate, refinement,
// contradiction or new for the ambiguous-band pair. Below the
// abstention band it takes the alternative (new); inside the defer
// band it falls back to the structural rule so admission never blocks
// on an uncertain judge.
type MemoryJudge struct {
	Dec    ChoiceDecider
	Thresh models.Thresholds
}

// NewMemoryJudge returns the admission judge. A nil Dec answers with
// the deterministic default (the memory service keeps its own).
func NewMemoryJudge(dec ChoiceDecider) *MemoryJudge {
	return &MemoryJudge{Dec: dec, Thresh: models.DefaultThresholds()}
}

// Classify implements memory.Decider.
func (j *MemoryJudge) Classify(ctx context.Context, c memory.Candidate, old memory.Memory) (string, error) {
	if j.Dec == nil {
		return memory.DeterministicDecider{}.Classify(ctx, c, old)
	}
	ans, err := j.Dec.Ask(ctx, models.AdmissionQuestion(c.Statement, old.Statement))
	if err != nil {
		return memory.DeterministicDecider{}.Classify(ctx, c, old)
	}
	outcome := models.Decide(ans.Prob, j.Thresh)
	if outcome == models.OutcomeAlternative {
		return "new", nil
	}
	if outcome == models.OutcomeDefer {
		return memory.DeterministicDecider{}.Classify(ctx, c, old)
	}
	switch strings.ToLower(strings.TrimSpace(ans.Choice)) {
	case "duplicate", "refinement", "contradiction", "new":
		return strings.ToLower(strings.TrimSpace(ans.Choice)), nil
	default:
		return memory.DeterministicDecider{}.Classify(ctx, c, old)
	}
}

// SkillScorer plugs the Section 19.4 skill-pertinence question into
// the skills index vector leg (Sections 12.3, 12.4): Score returns the
// rubric-scaled pertinence in (0, 1]. A nil or failing decider scores
// zero so selection degrades to lexical order.
type SkillScorer struct {
	Dec  ChoiceDecider
	Task string
}

// Score implements skills.VectorScorer.
func (s *SkillScorer) Score(query string, e *skills.Entry) float64 {
	if s.Dec == nil || e == nil {
		return 0
	}
	task := s.Task
	if task == "" {
		task = query
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ans, err := s.Dec.Ask(ctx, models.SkillScoreQuestion(e.Name, task))
	if err != nil {
		return 0
	}
	levels := 4.0
	if ans.Score < 0 {
		ans.Score = 0
	}
	if float64(ans.Score) >= levels {
		ans.Score = 3
	}
	return (float64(ans.Score) + 1) / levels * clampProb(ans.Prob)
}

// RerankTools reorders search_tools candidates through the Section
// 13.3 pertinence question, highest first. Ties keep input order.
func RerankTools(ctx context.Context, dec ChoiceDecider, query string, tools []string) []string {
	if dec == nil || len(tools) == 0 {
		return tools
	}
	type scored struct {
		name  string
		score float64
		idx   int
	}
	out := make([]scored, 0, len(tools))
	for i, name := range tools {
		score := 0.0
		if ans, err := dec.Ask(ctx, models.ToolRerankQuestion(name, query)); err == nil {
			score = (float64(ans.Score) + 1) / 4 * clampProb(ans.Prob)
		}
		out = append(out, scored{name: name, score: score, idx: i})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	names := make([]string, 0, len(tools))
	for _, s := range out {
		names = append(names, s.name)
	}
	return names
}

// JudgeStuck answers the Table 2.1 loop row (Sections 18.3, 19.4):
// from step 30 every 10 steps the decider judges "stuck", counting
// only at or above 0.9, and the verdict only ever fires the stage-1
// note — it never stops a run alone.
func JudgeStuck(ctx context.Context, dec ChoiceDecider, step int, state string) bool {
	if dec == nil {
		return false
	}
	q, ok := models.StuckQuestion(step, state)
	if !ok {
		return false
	}
	ans, err := dec.Ask(ctx, q)
	if err != nil {
		return false
	}
	return models.CountsStuck(ans.Prob)
}

// JudgeNotify answers the Table 2.1 notification row (Sections 5.4,
// 19.4): does this event deserve an immediate notice. A nil or
// failing decider keeps the deterministic policy (urgent at once).
func JudgeNotify(ctx context.Context, dec ChoiceDecider, event string) bool {
	if dec == nil {
		return false
	}
	ans, err := dec.Ask(ctx, models.NotifyQuestion(event))
	if err != nil {
		return false
	}
	return models.Decide(ans.Prob, models.DefaultThresholds()) == models.OutcomeAct
}

func clampProb(p float64) float64 {
	if p != p || p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}
