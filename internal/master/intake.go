package master

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Normalized is the channel output of intake step 1 (Section 6.1):
// plain text, voice transcripts with end-of-turn marks, attachments
// as blob references, reply links and the detected language.
type Normalized struct {
	RelayedBy   string
	CreatedAt   time.Time
	Context     *api.AppContext
	Role        string
	Changes     []api.FileChange
	Sources     []api.SourceRef
	Text        string
	Attachments []string
	ReplyTo     string
	Language    string
	Channel     string
	Sender      string
}

// Preanalysis is the deterministic scan of intake step 3: slash
// commands, pending answers, identifier refs, paths, URLs, project
// and agent mentions, and temporal expressions. No model involved.
type Preanalysis struct {
	Command       string
	CommandArgs   string
	IsCommand     bool
	AnswerTo      string // question or plan id this answers
	TaskRefs      []string
	RunRefs       []string
	Paths         []string
	URLs          []string
	Mentions      []Mention
	Temporal      []TemporalRef
	HasMandate    bool   // "remember …" style durable instruction
	ControlIntent string // pause|resume|cancel|stop|status|redirect|approve|deny|""
	ControlTarget string // raw target phrase
}

// TemporalRef is one interpreted temporal expression in the user zone.
type TemporalRef struct {
	Text string
	At   time.Time
}

var (
	reTaskRef = regexp.MustCompile(`\b[Tt](\d{1,6})\b`)
	reRunRef  = regexp.MustCompile(`\b[Rr](\d{1,6})\b`)
	reULIDRef = regexp.MustCompile(`\b(?:tsk|run|agt|cnv|msg|req|crt)_[0-9A-HJKMNP-TV-Z]{26}\b`)
	rePath    = regexp.MustCompile(`(?:^|[\s"'(])((?:\.{0,2}/|~/|/)[\w\-./]+|\b[\w\-.]+/[\w\-./]+)`)
	reURL     = regexp.MustCompile(`https?://[^\s<>"']+`)
)

// Temporal expressions handled deterministically: relative ("in 2
// hours", "tomorrow at 9"), weekday ("next tuesday") and clock times
// ("at 5", "9:00"). DST is correct because resolution runs in the
// user zone with time.LoadLocation semantics (Section 6.7).
func resolveTemporal(text string, now time.Time, loc *time.Location) []TemporalRef {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	lower := strings.ToLower(text)
	var out []TemporalRef
	add := func(expr string, at time.Time) {
		out = append(out, TemporalRef{Text: expr, At: at.In(loc)})
	}
	if idx := strings.Index(lower, "tomorrow"); idx >= 0 {
		base := time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, loc)
		if m := regexp.MustCompile(`tomorrow at (\d{1,2})(?::(\d{2}))?\s*(am|pm)?`).FindStringSubmatch(lower); m != nil {
			h, mi := atoi(m[1]), atoi2(m[2])
			h = applyMeridiem(h, m[3])
			base = time.Date(now.Year(), now.Month(), now.Day()+1, h, mi, 0, 0, loc)
		}
		add("tomorrow", base)
		_ = idx
	}
	if m := regexp.MustCompile(`in (\d+)\s*(minute|hour|day)s?`).FindStringSubmatch(lower); m != nil {
		n := atoi(m[1])
		d := time.Duration(n) * time.Minute
		switch m[2] {
		case "hour":
			d = time.Duration(n) * time.Hour
		case "day":
			d = time.Duration(n) * 24 * time.Hour
		}
		add(m[0], now.Add(d))
	}
	weekdays := map[string]time.Weekday{
		"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
		"wednesday": time.Wednesday, "thursday": time.Thursday, "friday": time.Friday,
		"saturday": time.Saturday,
	}
	for name, wd := range weekdays {
		pat := regexp.MustCompile(`(?:next\s+|this\s+)?` + name + `(?:\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?)?`)
		if m := pat.FindStringSubmatch(lower); m != nil {
			delta := (int(wd) - int(now.Weekday()) + 7) % 7
			if strings.HasPrefix(m[0], "next") && delta == 0 {
				delta = 7
			}
			if delta == 0 && m[1] != "" {
				h, mi := atoi(m[1]), atoi2(m[2])
				h = applyMeridiem(h, m[3])
				cand := time.Date(now.Year(), now.Month(), now.Day(), h, mi, 0, 0, loc)
				if cand.After(now) {
					add(m[0], cand)
					continue
				}
				delta = 7
			}
			h, mi := 9, 0
			if m[1] != "" {
				h, mi = atoi(m[1]), atoi2(m[2])
				h = applyMeridiem(h, m[3])
			}
			base := time.Date(now.Year(), now.Month(), now.Day()+delta, h, mi, 0, 0, loc)
			add(m[0], base)
		}
	}
	if m := regexp.MustCompile(`\bat\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`).FindStringSubmatch(lower); m != nil {
		if !strings.Contains(lower, "tomorrow") && !hasWeekday(lower) {
			h, mi := atoi(m[1]), atoi2(m[2])
			h = applyMeridiem(h, m[3])
			cand := time.Date(now.Year(), now.Month(), now.Day(), h, mi, 0, 0, loc)
			if !cand.After(now) {
				cand = cand.Add(24 * time.Hour)
			}
			add(m[0], cand)
		}
	}
	return out
}

func hasWeekday(s string) bool {
	for _, w := range []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func atoi2(s string) int {
	if s == "" {
		return 0
	}
	return atoi(s)
}

func applyMeridiem(h int, mer string) int {
	switch strings.ToLower(mer) {
	case "pm":
		if h < 12 {
			h += 12
		}
	case "am":
		if h == 12 {
			h = 0
		}
	}
	return h
}

// TriageType is the Section 6.1 message type.
type TriageType string

// Triage types.
const (
	TriageConversation TriageType = "conversation"
	TriageStatusQuery  TriageType = "status_query"
	TriageQuickAction  TriageType = "quick_action"
	TriageTask         TriageType = "task"
	TriageCorrection   TriageType = "correction"
	TriageSchedule     TriageType = "schedule"
	TriageMemoryWrite  TriageType = "memory_write"
	TriageConfig       TriageType = "config"
)

// Triage is the decider output over the preanalysis plus minimal
// context (last messages, active runs, candidate scopes).
type Triage struct {
	Type             TriageType
	Scopes           []string
	DurableFrag      []string
	NeedsClarify     bool
	Confidence       float64
	CorrectionTarget string
}

// Decider answers bounded triage questions. Implementations are the
// decision model (Section 19.4); tests use FakeDecider. Only potential
// fast-lane messages need classification before the master turn.
type Decider interface {
	Classify(ctx context.Context, text string, pre Preanalysis) Triage
}

// FakeDecider is a scriptable Decider for tests.
type FakeDecider struct {
	Fn    func(text string, pre Preanalysis) Triage
	Calls int
}

// Classify implements Decider.
func (f *FakeDecider) Classify(_ context.Context, text string, pre Preanalysis) Triage {
	f.Calls++
	if f.Fn != nil {
		return f.Fn(text, pre)
	}
	return Triage{Type: TriageConversation, Confidence: 1}
}

// Preanalyze runs the deterministic intake scan (Section 6.1 step 3).
// now and loc resolve temporal expressions in the user zone.
func Preanalyze(text string, lex *Lexicon, now time.Time, loc *time.Location) Preanalysis {
	var p Preanalysis
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "/") {
		fields := strings.Fields(trimmed)
		p.IsCommand = true
		p.Command = strings.TrimPrefix(fields[0], "/")
		if len(fields) > 1 {
			p.CommandArgs = strings.TrimSpace(trimmed[len(fields[0]):])
		}
	}
	for _, m := range reTaskRef.FindAllStringSubmatch(trimmed, -1) {
		p.TaskRefs = append(p.TaskRefs, "T"+m[1])
	}
	for _, m := range reRunRef.FindAllStringSubmatch(trimmed, -1) {
		p.RunRefs = append(p.RunRefs, "R"+m[1])
	}
	p.TaskRefs = append(p.TaskRefs, reULIDRef.FindAllString(trimmed, -1)...)
	for _, m := range rePath.FindAllStringSubmatch(trimmed, -1) {
		p.Paths = append(p.Paths, strings.Trim(m[1], "\"'"))
	}
	p.URLs = reURL.FindAllString(trimmed, -1)
	if lex != nil {
		p.Mentions = lex.Mentions(trimmed)
	}
	p.Temporal = resolveTemporal(trimmed, now, loc)
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "remember ") || strings.Contains(lower, "always ") || strings.Contains(lower, "never ") {
		p.HasMandate = true
	}
	p.ControlIntent, p.ControlTarget = detectControl(lower)
	return p
}

var controlVerbs = []struct {
	intent string
	words  []string
}{
	{"pause", []string{"pause", "stop ", "detén", "para ", "deten", "hold", "frena"}},
	{"resume", []string{"resume", "sigue", "continue", "reanud"}},
	{"cancel", []string{"cancel", "abort", "descarta", "discard"}},
	{"status", []string{"status", "qué está", "que esta", "how is", "what is", "cómo va", "como va", "progress"}},
	{"stop", []string{"/stop"}},
	{"approve", []string{"approve", "aprueba", "/approve", "lgtm"}},
	{"deny", []string{"deny", "rechaza", "/deny"}},
}

// detectControl maps natural language to a fast-lane intent with the
// raw target phrase. Ambiguity is NOT resolved here: the fast lane
// asks when the target matches several candidates (Section 6.1).
func detectControl(lower string) (intent, target string) {
	t := strings.TrimSpace(lower)
	if t == "/stop" || t == "stop" {
		return "stop", ""
	}
	if strings.HasPrefix(t, "/stop all") || t == "para todo" || strings.HasPrefix(t, "stop all") {
		return "stop_all", ""
	}
	for _, v := range controlVerbs {
		for _, w := range v.words {
			w = strings.TrimSpace(w)
			if w == "" {
				continue
			}
			if strings.HasPrefix(t, w) || strings.Contains(t, " "+w) {
				rest := strings.TrimSpace(strings.Replace(t, w, " ", 1))
				rest = strings.Trim(rest, " .!?")
				return v.intent, rest
			}
		}
	}
	return "", ""
}

// IntakeResult is one normalized message ready for the fast lane or
// a master turn.
type IntakeResult struct {
	Msg     Normalized
	Pre     Preanalysis
	Triage  Triage
	MsgID   types.MessageID
	SentAt  time.Time
	Decided bool // true when the decider completed before the deadline
	timing  intakeTiming
}

// Intake runs the Section 6.1 pipeline: normalize, persist with
// interactive priority, preanalyze, and triage only when the
// preanalysis identifies a potential fast-lane message. Ordinary
// conversation defers classification to the master turn.
func Intake(ctx context.Context, msg Normalized, persist func(Normalized) (types.MessageID, error), lex *Lexicon, dec Decider, now time.Time, loc *time.Location) (IntakeResult, error) {
	id, err := persist(msg)
	if err != nil {
		return IntakeResult{}, err
	}
	pre := Preanalyze(msg.Text, lex, now, loc)
	res := IntakeResult{Msg: msg, Pre: pre, MsgID: id, SentAt: now, Triage: Triage{Type: TriageConversation}}
	// Slash commands resolve in preanalysis alone. Natural-language
	// control and status queries still run the decider when one is
	// configured: the fast lane serves them only with high confidence
	// and a resolved target (Section 6.1). Without a decider the
	// master classifies inside its own turn (no extra LLM call).
	if pre.IsCommand {
		res.Triage = Triage{Type: TriageQuickAction, Confidence: 1}
		return res, nil
	}
	res.timing.gated = intakeSwitch("EXCALIBUR_INTAKE_GATED_TRIAGE") && !triageCandidate(msg.Text, pre)
	if s, ok := dec.(classifySkipper); ok && !res.timing.gated && s.SkipClassify(pre) {
		res.timing.gated = true
	}
	if dec != nil && !res.timing.gated {
		start := time.Now()
		if ctx.Err() == nil {
			res.timing.deciderCalls = 1
		}
		res.Triage, res.Decided = classifyIntake(ctx, dec, msg.Text, pre)
		res.timing.deciderDuration = time.Since(start)
	}
	return res, nil
}

// classifySkipper lets the master veto a classification the fast lane could
// never use, such as a status question while no run exists.
type classifySkipper interface {
	SkipClassify(pre Preanalysis) bool
}

// intakeSwitch defaults latency improvements on, including on invalid input.
func intakeSwitch(name string) bool {
	enabled, err := strconv.ParseBool(os.Getenv(name))
	return err != nil || enabled
}

func triageCandidate(text string, pre Preanalysis) bool {
	return pre.ControlIntent != "" || len(pre.TaskRefs) > 0 || len(pre.RunRefs) > 0 || looksLikeStatusQuery(text)
}

// A zero duration explicitly disables the deadline for rollback. Invalid
// values retain the default, so a typo cannot accidentally remove the bound.
func intakeDeciderTimeout() time.Duration {
	value, err := time.ParseDuration(os.Getenv("EXCALIBUR_INTAKE_DECIDER_TIMEOUT"))
	if err != nil || value < 0 {
		return 2500 * time.Millisecond
	}
	return value
}

func classifyIntake(ctx context.Context, dec Decider, text string, pre Preanalysis) (Triage, bool) {
	timeout := intakeDeciderTimeout()
	if timeout == 0 {
		return dec.Classify(ctx, text, pre), true
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if bounded.Err() != nil {
		return DeterministicTriage(text, pre), false
	}
	result := make(chan Triage, 1)
	safego.Go("intake-decider", nil, func() {
		result <- dec.Classify(bounded, text, pre)
	})
	select {
	case tri := <-result:
		// Do not act on a result arriving at or after the deadline.
		if bounded.Err() == nil {
			return tri, true
		}
	case <-bounded.Done():
	}
	// Keep deterministic metadata, but make the master handle the message.
	// A late answer can only write to the buffered channel, never run controls.
	return DeterministicTriage(text, pre), false
}

// DeterministicTriage supplies the model-free intake fallback (Section 6.1).
func DeterministicTriage(text string, pre Preanalysis) Triage {
	lower := strings.ToLower(strings.TrimSpace(text))
	tri := Triage{Type: TriageConversation, Confidence: 1}
	switch {
	case pre.IsCommand:
		tri.Type = TriageStatusQuery
	case pre.ControlIntent == "status":
		tri.Type = TriageStatusQuery
	case pre.ControlIntent == "pause" || pre.ControlIntent == "resume" ||
		pre.ControlIntent == "cancel" || pre.ControlIntent == "redirect" ||
		pre.ControlIntent == "stop" || pre.ControlIntent == "approve" ||
		pre.ControlIntent == "deny":
		tri.Type = TriageStatusQuery
		tri.CorrectionTarget = strings.TrimSpace(pre.ControlTarget)
		if pre.ControlIntent == "redirect" {
			tri.Type = TriageCorrection
		}
		if strings.TrimSpace(pre.ControlTarget) == "" {
			tri.NeedsClarify = true
		}
	case pre.HasMandate || strings.Contains(lower, "remember") || strings.Contains(lower, "remind"):
		tri.Type = TriageSchedule
		if pre.HasMandate {
			tri.DurableFrag = []string{text}
		}
	case len(pre.TaskRefs) > 0 || len(pre.Paths) > 0 || len(pre.URLs) > 0:
		tri.Type = TriageTask
	}
	return tri
}

// Timing describes the intake work, without message or provider data.
// Calls count Classify attempts, including timeouts; slash commands use zero.
type intakeTiming struct {
	gated           bool
	deciderCalls    int
	deciderDuration time.Duration
}

func (in IntakeResult) logTiming(fastLane bool) {
	if intakeSwitch("EXCALIBUR_INTAKE_TIMING") {
		slog.Info("intake timing", "gated", in.timing.gated,
			"decider_calls", in.timing.deciderCalls,
			"decider_ms", float64(in.timing.deciderDuration)/float64(time.Millisecond),
			"fast_lane", fastLane)
	}
}
