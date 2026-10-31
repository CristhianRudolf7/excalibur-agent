package master

import (
	"strings"
	"time"
)

// Voice contract of Section 6.6: the channel delivers transcript
// segments with end-of-turn detection, and a barge-in event when the
// user starts talking while the agent answers. Output streams to
// synthesis; long results stay written and voice only announces them.

// VoiceSegment is one transcript chunk from the channel.
type VoiceSegment struct {
	Text      string
	Final     bool // end-of-turn detected
	StartedAt time.Time
}

// BargeIn is emitted when the user talks over the agent answer.
type BargeIn struct {
	At time.Time
}

// VoiceStyle constrains spoken answers: short and natural, no long
// lists (skill voice-responses); long material stays as text.
type VoiceStyle struct {
	MaxSpokenChars int
}

// DefaultVoiceStyle caps spoken turns at a short paragraph.
func DefaultVoiceStyle() VoiceStyle { return VoiceStyle{MaxSpokenChars: 280} }

// SplitForVoice partitions an answer: the spoken head for synthesis
// and the written remainder left as a message or artifact.
func (v VoiceStyle) SplitForVoice(answer string) (spoken, written string) {
	if len(answer) <= v.MaxSpokenChars {
		return answer, ""
	}
	cut := strings.LastIndex(answer[:v.MaxSpokenChars], ". ")
	if cut < 0 {
		cut = v.MaxSpokenChars
	} else {
		cut++
	}
	return strings.TrimSpace(answer[:cut]), strings.TrimSpace(answer[cut:])
}

// VoiceOutcome is what the channel must do on barge-in.
type VoiceOutcome struct {
	// StopSynthesis is always true: cut the audio at once.
	StopSynthesis bool
	// CancelGeneration cancels the in-flight model call.
	CancelGeneration bool
	// NewMessage is the speech so far, handled as a new message per
	// Section 5.3 (cancel under 2 s without shown output, else inject
	// at the next safe point).
	NewMessage string
}

// OnBargeIn implements the Section 6.6 interruption contract.
func OnBargeIn(speechSoFar string) VoiceOutcome {
	return VoiceOutcome{StopSynthesis: true, CancelGeneration: true, NewMessage: speechSoFar}
}
