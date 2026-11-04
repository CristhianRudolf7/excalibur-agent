package api

import "github.com/CristhianRudolf7/excalibur-agent/internal/types"

const (
	// EventBrowserSession announces that a conversation's browser became
	// visible (first page work) or ended.
	EventBrowserSession types.EventType = "browser.session"
	// EventBrowserActivity reports what the agent is doing in the page.
	EventBrowserActivity types.EventType = "browser.activity"

	// MethodBrowserLiveState reads a conversation's current live browser.
	MethodBrowserLiveState = "browser.live_state"
)

// Browser session states carried by BrowserSessionEvent.State.
const (
	BrowserSessionActive = "active"
	BrowserSessionEnded  = "ended"
)

// Browser activity phases carried by BrowserActivityEvent.Phase.
const (
	BrowserPhaseStart  = "start"
	BrowserPhaseDone   = "done"
	BrowserPhaseFailed = "failed"
)

// Browser activity kinds carried by BrowserActivityEvent.Kind.
const (
	BrowserKindNavigate     = "navigate"
	BrowserKindWait         = "wait"
	BrowserKindRead         = "read"
	BrowserKindClick        = "click"
	BrowserKindType         = "type"
	BrowserKindTypePassword = "type_password"
	BrowserKindSelect       = "select"
	BrowserKindHover        = "hover"
	BrowserKindScroll       = "scroll"
	BrowserKindPress        = "press"
	BrowserKindUpload       = "upload"
	BrowserKindDrag         = "drag"
	BrowserKindDownload     = "download"
	BrowserKindPage         = "page"
)

// BrowserRect is a CSS pixel rectangle in the page viewport.
type BrowserRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// BrowserSessionEvent never carries a query string or fragment in URL.
type BrowserSessionEvent struct {
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id"`
	SessionID      string `json:"session_id"`
	State          string `json:"state"`
	URL            string `json:"url,omitempty"`
	Title          string `json:"title,omitempty"`
}

// BrowserActivityEvent describes one agent action. It never carries typed
// values: Target is the element's accessible name with known secrets removed,
// and URL has no query string or fragment.
type BrowserActivityEvent struct {
	ConversationID string       `json:"conversation_id"`
	RunID          string       `json:"run_id"`
	SessionID      string       `json:"session_id"`
	TabID          string       `json:"tab_id,omitempty"`
	Seq            uint64       `json:"seq"`
	Phase          string       `json:"phase"`
	Kind           string       `json:"kind"`
	Target         string       `json:"target,omitempty"`
	URL            string       `json:"url,omitempty"`
	Title          string       `json:"title,omitempty"`
	Box            *BrowserRect `json:"box,omitempty"`
}

// BrowserLiveStateParams selects one conversation.
type BrowserLiveStateParams struct {
	ConversationID string `json:"conversation_id"`
}

// BrowserLiveStateResult restores the panel after a reload. Session is absent
// when the conversation has no visible browser session.
type BrowserLiveStateResult struct {
	Session  *BrowserSessionEvent  `json:"session,omitempty"`
	Activity *BrowserActivityEvent `json:"activity,omitempty"`
}
