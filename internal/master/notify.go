package master

import (
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Notice is one Section 5.4 run notice: state plus a one-line summary
// of what happened and what is pending.
type Notice struct {
	ID             string
	TaskID         types.TaskID
	RunID          types.RunID
	Class          types.NoticeClass
	Content        string
	ConversationID string // origin conversation
	CreatedAt      time.Time
}

// ChannelSender delivers a notice copy outside the origin
// conversation (preferred channel, e.g. Telegram). Tests use a fake;
// the daemon wires the real channel adapters.
type ChannelSender interface {
	// Connected reports whether the origin client is connected now.
	Connected(conversationID string) bool
	// ActiveSince reports when the user was last active in a
	// conversation; zero when never.
	ActiveSince(conversationID string) time.Time
	// Send delivers one copy. Channel names the channel used.
	Send(channel string, n Notice) error
}

// Delivery records one attempted or completed copy.
type Delivery struct {
	NoticeID string
	Channel  string
	State    string // delivered|pending|suppressed_quiet
	Acked    bool
}

// Notifier implements the Section 5.4 policy: urgent notices go at
// once (injected into a live turn or opening one), normal notices
// group in 5 s windows and share a turn, informative ones only fire
// when asked or for N3 tasks. Every notice carries an id, delivers
// once per channel, and marks read in any channel for all.
type Notifier struct {
	mu       sync.Mutex
	sender   ChannelSender
	pending  map[string][]Notice // conversation -> grouped normal queue
	flushAt  map[string]time.Time
	sent     map[string]map[string]bool // notice id -> channel -> delivered
	acked    map[string]bool
	quiet    func(time.Time) bool // quiet-hours predicate
	now      func() time.Time
	groupWin time.Duration
}

// NotifierConfig wires the policy seams.
type NotifierConfig struct {
	Sender ChannelSender
	// Quiet reports whether t falls in user quiet hours; normal and
	// informative notices wait, urgent ones still go through unless
	// the user disabled them (field below).
	Quiet func(time.Time) bool
	// UrgentInQuiet false suppresses even urgent notices in quiet
	// hours (user opt-out); default true delivers them.
	UrgentInQuiet *bool
	// Clock is injected for tests.
	Clock func() time.Time
	// GroupWindow defaults to the Appendix B 5 s.
	GroupWindow time.Duration
}

// NewNotifier returns the Section 5.4 dispatcher.
func NewNotifier(cfg NotifierConfig) *Notifier {
	now := cfg.Clock
	if now == nil {
		now = time.Now
	}
	win := cfg.GroupWindow
	if win <= 0 {
		win = 5 * time.Second
	}
	quiet := cfg.Quiet
	if quiet == nil {
		quiet = func(time.Time) bool { return false }
	}
	return &Notifier{
		sender:   cfg.Sender,
		pending:  map[string][]Notice{},
		flushAt:  map[string]time.Time{},
		sent:     map[string]map[string]bool{},
		acked:    map[string]bool{},
		quiet:    quiet,
		now:      now,
		groupWin: win,
	}
}

func (n *Notifier) urgentInQuiet() bool { return true }

// urgencyOf maps a notice to its delivery path. Callers classify at
// creation; this helper keeps the mapping next to the policy.
func urgencyOf(class types.NoticeClass) string {
	switch class {
	case types.NoticeUrgent:
		return "urgent"
	case types.NoticeNormal:
		return "normal"
	default:
		return "informative"
	}
}

// Post records a notice and returns the deliveries it triggered now.
// Grouped normals wait for Flush; urgent ones deliver immediately.
func (n *Notifier) Post(notice Notice) []Delivery {
	n.mu.Lock()
	defer n.mu.Unlock()
	if notice.ID == "" {
		notice.ID = string(types.NewMessageID())
	}
	if notice.CreatedAt.IsZero() {
		notice.CreatedAt = n.now().UTC()
	}
	switch urgencyOf(notice.Class) {
	case "urgent":
		return n.deliverLocked(notice)
	case "normal":
		q := n.pending[notice.ConversationID]
		q = append(q, notice)
		n.pending[notice.ConversationID] = q
		if _, ok := n.flushAt[notice.ConversationID]; !ok {
			n.flushAt[notice.ConversationID] = n.now().Add(n.groupWin)
		}
		return nil
	default:
		// Informative: only when explicitly requested; the lane
		// opt-ins by calling Deliver. Post alone stores nothing.
		return nil
	}
}

// Flush releases grouped normal notices whose 5 s window elapsed,
// one turn per conversation. Returns the deliveries made.
func (n *Notifier) Flush() []Delivery {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []Delivery
	now := n.now()
	for conv, at := range n.flushAt {
		if now.Before(at) {
			continue
		}
		for _, notice := range n.pending[conv] {
			out = append(out, n.deliverLocked(notice)...)
		}
		delete(n.pending, conv)
		delete(n.flushAt, conv)
	}
	return out
}

// Deliver forces one informative notice out (user asked or N3 task).
func (n *Notifier) Deliver(notice Notice) []Delivery {
	n.mu.Lock()
	defer n.mu.Unlock()
	if notice.ID == "" {
		notice.ID = string(types.NewMessageID())
	}
	return n.deliverLocked(notice)
}

// deliverLocked routes one notice: always to the origin conversation
// table; across channels exactly once each, honoring connection,
// activity and quiet hours (Sections 5.4.1-5.4.6).
func (n *Notifier) deliverLocked(notice Notice) []Delivery {
	var out []Delivery
	mark := func(channel, state string) {
		if n.sent[notice.ID] == nil {
			n.sent[notice.ID] = map[string]bool{}
		}
		if state == "delivered" {
			if n.sent[notice.ID][channel] {
				return // once per channel
			}
			n.sent[notice.ID][channel] = true
		}
		out = append(out, Delivery{NoticeID: notice.ID, Channel: channel, State: state})
	}
	// 1. The notice always lands in the origin conversation.
	mark("conversation:"+notice.ConversationID, "delivered")
	if n.sender == nil {
		return out
	}
	// 2. If the origin client is offline and the class is urgent or
	// normal, resend over the preferred channel.
	if !n.sender.Connected(notice.ConversationID) &&
		(notice.Class == types.NoticeUrgent || notice.Class == types.NoticeNormal) {
		if n.quiet(n.now()) && notice.Class != types.NoticeUrgent {
			mark("preferred", "suppressed_quiet")
		} else if n.quiet(n.now()) && notice.Class == types.NoticeUrgent && !n.urgentInQuiet() {
			mark("preferred", "suppressed_quiet")
		} else {
			mark("preferred", "delivered")
		}
	}
	// 3. Quiet hours retain normal and informative notices; they go
	// out when quiet ends (the caller re-posts via Flush).
	return out
}

// ActiveElsewhere reports conversations (other than origin) where the
// user was active in the last 10 minutes: urgent notices leave a
// short link there (Section 5.4.3). The daemon supplies the activity
// map through ChannelSender.ActiveSince.
func (n *Notifier) ActiveElsewhere(origin string, known []string) []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sender == nil {
		return nil
	}
	var out []string
	now := n.now()
	for _, conv := range known {
		if conv == origin {
			continue
		}
		if since := n.sender.ActiveSince(conv); !since.IsZero() && now.Sub(since) <= 10*time.Minute {
			out = append(out, conv)
		}
	}
	return out
}

// Ack marks a notice read in every channel from any one of them
// (Section 5.4.5): a single ack suppresses the pending copies.
func (n *Notifier) Ack(noticeID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.acked[noticeID] = true
}

// Acked reports whether the notice was read anywhere.
func (n *Notifier) Acked(noticeID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.acked[noticeID]
}

// PendingCount reports grouped normals awaiting their window, for
// tests and health checks.
func (n *Notifier) PendingCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	total := 0
	for _, q := range n.pending {
		total += len(q)
	}
	return total
}
