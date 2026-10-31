package orchestrator

import (
	"context"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// Model gateway lanes of Section 5.2: P0 master turns, P1 user
// executions, P2 scheduled and background, P3 maintenance. One
// Gateway actor guards one provider: requests serialize in arrival
// order, P0 keeps a reserved concurrency slot plus 10% of the
// per-minute quota, a lower lane only takes a slot when no higher
// lane waits, and P3 yields while the user is active.

// gatewayMsg is one actor inbox message; the loop goroutine owns all
// scheduling state.
type gatewayMsg struct {
	req      *laneRequest
	release  bool
	active   *bool
	statsReq chan gatewayStats
}

type laneRequest struct {
	lane  Lane
	grant chan struct{}
	ctx   context.Context
}

// gatewayStats is the observable scheduler state for tests.
type gatewayStats struct {
	InFlight int
	Pending  [4]int
	Admitted [4]int
}

// Gateway is the Section 5.1 per-provider actor with priority lanes.
type Gateway struct {
	concurrency int
	perMinute   int
	now         func() time.Time

	inbox chan gatewayMsg
}

// NewGateway returns a gateway with total concurrency and
// per-minute quotas. reservation for P0 applies inside.
func NewGateway(concurrency, perMinute int) *Gateway {
	return &Gateway{
		concurrency: concurrency,
		perMinute:   perMinute,
		now:         func() time.Time { return time.Now().UTC() },
		inbox:       make(chan gatewayMsg, 128),
	}
}

// Start launches the actor loop; it ends with ctx. Every long-lived
// goroutine uses safego.Go (Section 3.2).
func (g *Gateway) Start(ctx context.Context) {
	safego.Go("gateway", nil, func() {
		var inFlight int
		var minute []time.Time
		var pending [4][]laneRequest
		var admitted [4]int
		var userActive bool
		prune := func(now time.Time) {
			cut := now.Add(-time.Minute)
			kept := minute[:0]
			for _, t := range minute {
				if t.After(cut) {
					kept = append(kept, t)
				}
			}
			minute = kept
		}
		admit := func(now time.Time) {
			prune(now)
			for {
				progress := false
				for lane := LaneP0; lane <= LaneP3; lane++ {
					// Drop canceled waiters at the head.
					for len(pending[lane]) > 0 {
						select {
						case <-pending[lane][0].ctx.Done():
							pending[lane] = pending[lane][1:]
						default:
							goto head
						}
					}
				head:
					if len(pending[lane]) == 0 {
						continue
					}
					if lane == LaneP3 && userActive {
						continue
					}
					if inFlight >= g.concurrency || len(minute) >= g.perMinute {
						continue
					}
					if lane != LaneP0 {
						// P0 reservation: background lanes leave one
						// slot and 10% of the minute quota free.
						if inFlight+1 > g.concurrency-1 {
							continue
						}
						if float64(len(minute)+1) > 0.9*float64(g.perMinute) {
							continue
						}
					}
					head := pending[lane][0]
					pending[lane] = pending[lane][1:]
					inFlight++
					minute = append(minute, now)
					admitted[lane]++
					close(head.grant)
					progress = true
				}
				if !progress {
					return
				}
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-g.inbox:
				switch {
				case m.req != nil:
					pending[m.req.lane] = append(pending[m.req.lane], *m.req)
					admit(g.now())
				case m.release:
					if inFlight > 0 {
						inFlight--
					}
					admit(g.now())
				case m.active != nil:
					userActive = *m.active
					admit(g.now())
				case m.statsReq != nil:
					var p [4]int
					for lane := LaneP0; lane <= LaneP3; lane++ {
						p[lane] = len(pending[lane])
					}
					m.statsReq <- gatewayStats{InFlight: inFlight, Pending: p, Admitted: admitted}
				}
			}
		}
	})
}

// Request enqueues one lane acquisition; the returned channel closes
// when the slot is granted. Canceling ctx drops the waiter.
func (g *Gateway) Request(ctx context.Context, lane Lane) <-chan struct{} {
	grant := make(chan struct{})
	select {
	case g.inbox <- gatewayMsg{req: &laneRequest{lane: lane, grant: grant, ctx: ctx}}:
	case <-ctx.Done():
		close(grant)
	}
	return grant
}

// Release frees one slot and re-runs admission.
func (g *Gateway) Release() {
	select {
	case g.inbox <- gatewayMsg{release: true}:
	default:
	}
}

// SetUserActive marks user activity: P3 yields while true.
func (g *Gateway) SetUserActive(active bool) {
	g.inbox <- gatewayMsg{active: &active}
}

// Stats snapshots the scheduler state.
func (g *Gateway) Stats() gatewayStats {
	reply := make(chan gatewayStats, 1)
	g.inbox <- gatewayMsg{statsReq: reply}
	return <-reply
}
