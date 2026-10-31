package telemetry

import (
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// SampleInterval is the per-task RSS sampling period of Section 22.2:
// the daemon RSS is sampled every second during the run.
const SampleInterval = time.Second

// Sample is one retained resource observation for the Appendix F
// minimum records (resource samples per trial). Daemon RSS and child
// peaks are recorded by different paths and never mixed: this sampler
// tracks the daemon, while child peaks arrive from the cgroup v2
// memory.peak counter via ObserveResources.
type Sample struct {
	At            time.Time `json:"at"`
	DaemonRSS     uint64    `json:"daemon_rss_bytes"`
	CPUSeconds    float64   `json:"cpu_seconds"`
	Processes     int       `json:"processes"`
	ChildOOMKills int       `json:"child_oom_kills"`
}

// Sampler polls a platform.ResourceSampler every interval and feeds
// daemon RSS/CPU into TaskMetrics. The polling loop is a long-lived
// goroutine owned by safego.Go; Stop ends it. The zero interval
// selects SampleInterval.
type Sampler struct {
	src      platform.ResourceSampler
	metrics  *TaskMetrics
	interval time.Duration
	pid      int

	mu      sync.Mutex
	samples []Sample

	stop   chan struct{}
	done   chan struct{}
	stopMu sync.Mutex
	stoped bool
}

// NewSampler returns a sampler attributing daemon samples to pid (the
// tool-process group leader; 0 samples the daemon only).
func NewSampler(src platform.ResourceSampler, metrics *TaskMetrics, pid int, interval time.Duration) *Sampler {
	if interval <= 0 {
		interval = SampleInterval
	}
	return &Sampler{src: src, metrics: metrics, pid: pid, interval: interval}
}

// Start begins polling in a safego-owned goroutine and takes the first
// sample synchronously so short runs still record one.
func (s *Sampler) Start(owner string) {
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	s.sample(time.Now().UTC())
	safego.Go(owner, nil, func() {
		defer close(s.done)
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case now := <-t.C:
				s.sample(now.UTC())
			}
		}
	})
}

// Stop ends polling and waits for the loop to exit.
func (s *Sampler) Stop() {
	s.stopMu.Lock()
	if !s.stoped {
		s.stoped = true
		close(s.stop)
	}
	s.stopMu.Unlock()
	<-s.done
}

// Samples returns a copy of the retained observations.
func (s *Sampler) Samples() []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sample(nil), s.samples...)
}

func (s *Sampler) sample(now time.Time) {
	rs, err := s.src.Sample(s.pid)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.samples = append(s.samples, Sample{
		At: now, DaemonRSS: rs.RSSBytes,
		CPUSeconds: rs.CPUSeconds, Processes: rs.Processes, ChildOOMKills: rs.OOMKills,
	})
	s.mu.Unlock()
	s.metrics.ObserveDaemon(rs.RSSBytes, rs.CPUSeconds)
}
