package telemetry

import (
	"fmt"
	"strings"
	"time"
)

// Alert is one watchdog firing. Alerts go to the master, which decides
// whether to inform the user (Section 22.5).
type Alert struct {
	At       time.Time `json:"at"`
	Watchdog string    `json:"watchdog"`
	Severity string    `json:"severity"`
	Detail   string    `json:"detail"`
}

// Thresholds configures every health watchdog. Defaults come from
// Appendix B and Section 22.5.
type Thresholds struct {
	// StuckAfter declares a run stuck after this heartbeat silence.
	StuckAfter time.Duration
	// WALAlertBytes warns above this WAL size (Appendix B: 256MiB).
	WALAlertBytes int64
	// RAMHighBytes pauses background work above this RSS.
	RAMHighBytes uint64
	// DiskFreeLowBytes warns below this free disk.
	DiskFreeLowBytes uint64
	// ProviderErrorRate opens above this 1-minute error rate.
	ProviderErrorRate float64
	// ProviderFailures opens after this many consecutive failures.
	ProviderFailures int
	// CacheDriftWarnAt warns when measured hit rate falls this far
	// below the structural expectation of Section 11.6.
	CacheDriftWarnAt float64
}

// DefaultThresholds returns the Appendix B initial values.
func DefaultThresholds() Thresholds {
	return Thresholds{
		StuckAfter:        10 * time.Minute,
		WALAlertBytes:     256 << 20,
		RAMHighBytes:      4 << 30,
		DiskFreeLowBytes:  1 << 30,
		ProviderErrorRate: 0.5,
		ProviderFailures:  5,
		CacheDriftWarnAt:  0.15,
	}
}

// CheckStuck fires when now minus lastHeartbeat exceeds StuckAfter.
func CheckStuck(th Thresholds, runID string, lastHeartbeat, now time.Time) *Alert {
	if now.Sub(lastHeartbeat) <= th.StuckAfter {
		return nil
	}
	return &Alert{
		At:       now.UTC(),
		Watchdog: "stuck_run",
		Severity: "urgent",
		Detail:   fmt.Sprintf("run %s silent for %s", runID, now.Sub(lastHeartbeat).Round(time.Second)),
	}
}

// CheckWAL fires when the WAL file exceeds the alert size.
func CheckWAL(th Thresholds, walBytes int64, now time.Time) *Alert {
	if walBytes <= th.WALAlertBytes {
		return nil
	}
	return &Alert{
		At:       now.UTC(),
		Watchdog: "wal_size",
		Severity: "normal",
		Detail:   fmt.Sprintf("WAL size %d bytes exceeds %d", walBytes, th.WALAlertBytes),
	}
}

// CheckRAM fires when RSS exceeds the high-water mark.
func CheckRAM(th Thresholds, rssBytes uint64, now time.Time) *Alert {
	if rssBytes <= th.RAMHighBytes {
		return nil
	}
	return &Alert{
		At:       now.UTC(),
		Watchdog: "ram_pressure",
		Severity: "urgent",
		Detail:   fmt.Sprintf("RSS %d bytes exceeds %d", rssBytes, th.RAMHighBytes),
	}
}

// CheckDisk fires when free disk drops below the floor.
func CheckDisk(th Thresholds, freeBytes uint64, now time.Time) *Alert {
	if freeBytes >= th.DiskFreeLowBytes {
		return nil
	}
	return &Alert{
		At:       now.UTC(),
		Watchdog: "disk_low",
		Severity: "urgent",
		Detail:   fmt.Sprintf("free disk %d bytes below %d", freeBytes, th.DiskFreeLowBytes),
	}
}

// CheckProvider fires on consecutive failures or a high 1-minute error
// rate (same rule as the Section 19.5 breaker).
func CheckProvider(th Thresholds, consecutiveFailures int, errorRate float64, provider string, now time.Time) *Alert {
	if consecutiveFailures >= th.ProviderFailures {
		return &Alert{
			At:       now.UTC(),
			Watchdog: "provider_errors",
			Severity: "urgent",
			Detail:   fmt.Sprintf("provider %s: %d consecutive failures", provider, consecutiveFailures),
		}
	}
	if errorRate > th.ProviderErrorRate {
		return &Alert{
			At:       now.UTC(),
			Watchdog: "provider_errors",
			Severity: "urgent",
			Detail:   fmt.Sprintf("provider %s: 1-minute error rate %.2f", provider, errorRate),
		}
	}
	return nil
}

// CheckCacheDrift fires when the measured cache hit rate falls below
// the structural expectation by more than the tolerance (Section 11.6).
func CheckCacheDrift(th Thresholds, expected, measured float64, now time.Time) *Alert {
	if expected-measured <= th.CacheDriftWarnAt {
		return nil
	}
	return &Alert{
		At:       now.UTC(),
		Watchdog: "cache_drift",
		Severity: "normal",
		Detail:   fmt.Sprintf("cache hit %.2f vs expected %.2f", measured, expected),
	}
}

// Summarize renders alerts as one master-facing line.
func Summarize(alerts []Alert) string {
	if len(alerts) == 0 {
		return "all watchdogs clear"
	}
	parts := make([]string, 0, len(alerts))
	for _, a := range alerts {
		parts = append(parts, a.Watchdog+": "+a.Detail)
	}
	return strings.Join(parts, "; ")
}
