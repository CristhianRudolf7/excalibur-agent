package master

import (
	"strings"
	"time"
)

// Ask-or-assume implements Section 6.4: every doubt is scored, and
// the lane either asks (at most 3 questions per message, each with
// the default the lane would take) or assumes and records the
// assumption in the request file.
//
//   - missing mandatory data without a sane default -> blocking ask;
//   - irreversible or externally visible action with doubt -> blocking ask;
//   - materially different readings with principal probability < 0.7
//     and costly rework -> non-blocking ask;
//   - otherwise -> assume and record.
type Doubt struct {
	Topic string
	// Mandatory has no sane default (recipient, credential, target
	// directory, business decision).
	Mandatory bool
	// Irreversible affects the outside (send, publish, pay, delete,
	// deploy) and the doubt touches it.
	Irreversible bool
	// PrincipalProb is the decider probability of the leading reading.
	PrincipalProb float64
	// ReworkCostly is true when guessing wrong costs more than a few
	// minutes of agent work.
	ReworkCostly bool
	// Default is the option the lane takes without an answer.
	Default string
}

// Question is one grouped user question.
type Question struct {
	ID       string
	Topic    string
	Blocking bool
	Default  string
	AskedAt  time.Time
}

// AskDecision is the lane outcome for one message.
type AskDecision struct {
	Questions   []Question
	Assumptions []Assumption
}

// Assumption is a recorded supposition with its justification.
type Assumption struct {
	Text   string
	Reason string
}

// NonBlockingTimeout is the Appendix B 15 minutes after which a
// non-blocking question without other work takes its default.
const NonBlockingTimeout = 15 * time.Minute

// BlockingReminder is the Appendix B 24 h reminder for blocking waits.
const BlockingReminder = 24 * time.Hour

// MaxQuestionsPerMessage is the Appendix B cap of 3.
const MaxQuestionsPerMessage = 3

// AskOrAssume applies the Section 6.4 rule to every doubt.
func AskOrAssume(doubts []Doubt, now time.Time) AskDecision {
	var d AskDecision
	for _, doubt := range doubts {
		switch {
		case doubt.Mandatory || doubt.Irreversible:
			d.Questions = append(d.Questions, Question{ID: "q_" + slug(doubt.Topic), Topic: doubt.Topic, Blocking: true, Default: doubt.Default, AskedAt: now})
		case doubt.PrincipalProb < 0.7 && doubt.ReworkCostly:
			d.Questions = append(d.Questions, Question{ID: "q_" + slug(doubt.Topic), Topic: doubt.Topic, Blocking: false, Default: doubt.Default, AskedAt: now})
		default:
			reason := "trivial or low-cost doubt; assuming the leading reading"
			if doubt.PrincipalProb >= 0.7 {
				reason = "leading reading is clear; assuming it"
			}
			d.Assumptions = append(d.Assumptions, Assumption{Text: doubt.Topic + " (assumed: " + doubt.Default + ")", Reason: reason})
		}
		if len(d.Questions) >= MaxQuestionsPerMessage {
			// The rest becomes assumptions with the same visibility:
			// grouped questions never exceed 3 per message.
			for _, rest := range doubts[len(d.Questions):] {
				d.Assumptions = append(d.Assumptions, Assumption{Text: rest.Topic + " (assumed: " + rest.Default + ")", Reason: "question budget exhausted; assuming default"})
			}
			break
		}
	}
	return d
}

// DueDefault reports whether a non-blocking question takes its
// default: 15 min without an answer and the run has nothing else to
// do (Section 6.4).
func DueDefault(q Question, now time.Time, hasOtherWork bool) bool {
	if q.Blocking || hasOtherWork {
		return false
	}
	return !now.Before(q.AskedAt.Add(NonBlockingTimeout))
}

// DueReminder reports whether a blocking wait needs its 24 h nudge.
func DueReminder(q Question, now time.Time) bool {
	if !q.Blocking {
		return false
	}
	return !now.Before(q.AskedAt.Add(BlockingReminder))
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		out = "doubt"
	}
	return out
}
