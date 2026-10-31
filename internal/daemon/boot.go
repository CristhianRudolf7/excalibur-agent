package daemon

import (
	"context"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// RunView is the minimal run row adoption needs.
type RunView struct {
	RunID     string
	State     types.RunState
	UpdatedAt time.Time
	Step      string // engine step label for quarantine matching
}

// Adoption reconciles non-terminal runs at boot (Section 17.3). The
// daemon lists candidates through ListRuns; Decide maps each to its
// disposition; Apply carries it out (resume, pause, block, keep).
type Adoption interface {
	ListRuns() []RunView
	Decide(run RunView, downFor time.Duration) AdoptDecision
	Apply(run RunView, decision AdoptDecision) error
}

// AdoptDecision names the Section 17.3 continuation.
type AdoptDecision string

// Adoption dispositions.
const (
	AdoptResume  AdoptDecision = "resume"  // <6h, reconciled, no uncertainty
	AdoptPause   AdoptDecision = "pause"   // recovery: long outage or low budget
	AdoptBlock   AdoptDecision = "block"   // uncertain effect: urgent notice
	AdoptWaiting AdoptDecision = "waiting" // user wait survives with its ask intact
	AdoptKeep    AdoptDecision = "keep"    // already terminal or safe mode
)

// AdoptionSummary counts one boot pass for the single master notice.
type AdoptionSummary struct {
	Resumed int
	Paused  int
	Blocked int
	Waiting int
	Kept    int
}

// DefaultDecide implements the Section 17.3 table: user waits
// survive, uncertain effects block, long outages pause, the rest
// resumes with a recovery note.
func DefaultDecide(run RunView, downFor time.Duration, uncertain bool, waitingUser bool) AdoptDecision {
	switch {
	case waitingUser:
		return AdoptWaiting
	case uncertain:
		return AdoptBlock
	case run.State == types.RunPaused && false:
		return AdoptKeep
	case downFor > 6*time.Hour:
		return AdoptPause
	case run.State.IsTerminal():
		return AdoptKeep
	default:
		return AdoptResume
	}
}

// adopt runs the Section 17.3 pass: safe mode pauses everything with
// the recovery reason, otherwise each run goes through Decide/Apply.
// It returns the summary the master reports as the single notice.
func (d *Daemon) adopt(_ context.Context, now time.Time) (AdoptionSummary, error) {
	var sum AdoptionSummary
	if d.dep.Adoption == nil {
		return sum, nil
	}
	d.mu.Lock()
	safe := d.safeMode
	d.mu.Unlock()
	downFor := d.downtime(now)
	for _, run := range d.dep.Adoption.ListRuns() {
		var dec AdoptDecision
		if safe {
			dec = AdoptPause
		} else {
			dec = d.dep.Adoption.Decide(run, downFor)
		}
		if err := d.dep.Adoption.Apply(run, dec); err != nil {
			continue
		}
		switch dec {
		case AdoptResume:
			sum.Resumed++
		case AdoptPause:
			sum.Paused++
		case AdoptBlock:
			sum.Blocked++
		case AdoptWaiting:
			sum.Waiting++
		default:
			sum.Kept++
		}
	}
	return sum, nil
}

// downtime estimates the outage from the crash-history file; without
// history it reports zero (clean first start).
func (d *Daemon) downtime(now time.Time) time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.crashes) == 0 {
		return 0
	}
	last := d.crashes[len(d.crashes)-1]
	if now.Before(last) {
		return 0
	}
	return now.Sub(last)
}
