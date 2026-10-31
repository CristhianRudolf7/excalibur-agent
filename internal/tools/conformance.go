package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Conformance suite (Section 14.12, excalibur ext test). The suite is
// the gate for activating a store extension and the check behind the
// create-tool skill: the extension contract (valid manifest, schemas),
// the Section 14.1 states, idempotency under the same key, cancel
// honored per the declared cancel class, dry_run for effectful tools,
// and outputs inside output_budget.

// ConformanceProbe is one behavioral probe of an extension tool. Args
// must be valid for the tool schema; the suite invokes the tool twice
// with the same idempotency key and compares the outcomes.
type ConformanceProbe struct {
	Tool string
	Args json.RawMessage
}

// ConformanceCheck is one suite verdict.
type ConformanceCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// ConformanceReport is the suite outcome.
type ConformanceReport struct {
	Extension string             `json:"extension"`
	Passed    bool               `json:"passed"`
	Checks    []ConformanceCheck `json:"checks"`
}

// RunConformance executes the contract suite of one extension over its
// registered tools. reg and exec run the same pipeline production
// calls use; probes supply the behavioral cases per tool.
func RunConformance(ctx context.Context, reg Registry, exec *Executor, ext *LoadedExtension, probes []ConformanceProbe) ConformanceReport {
	report := ConformanceReport{Extension: ext.Manifest.ID, Passed: true}
	fail := func(name, detail string) {
		report.Checks = append(report.Checks, ConformanceCheck{Name: name, Passed: false, Detail: detail})
		report.Passed = false
	}
	pass := func(name string) {
		report.Checks = append(report.Checks, ConformanceCheck{Name: name, Passed: true})
	}
	// 1. Manifest: valid, supported contract, known kind, unique names.
	if err := ext.Manifest.Validate(); err != nil {
		fail("manifest_valid", err.Error())
	} else {
		pass("manifest_valid")
	}
	// Static checks run over the activated tools: for route 1 the
	// manifest declares no static entries and binding fills them from
	// the server tools/list.
	active := ext.Tools()
	defs := make([]Definition, 0, len(active))
	for _, t := range active {
		defs = append(defs, t.Definition())
	}
	if len(defs) == 0 {
		fail("tools", "extension activated no tools")
		return report
	}
	seen := map[string]bool{}
	dup := false
	for _, def := range defs {
		if seen[def.Name] {
			dup = true
		}
		seen[def.Name] = true
		if err := def.Validate(); err != nil {
			fail("schema:"+def.Name, err.Error())
		} else {
			pass("schema:" + def.Name)
		}
		if def.Timeout <= 0 {
			fail("timeout:"+def.Name, "timeout must be positive")
		}
		if def.OutputBudget <= 0 {
			fail("output_budget:"+def.Name, "output_budget must be positive")
		}
		// Effectful tools must declare no-effects behavior: the
		// package tests and the store gate run them without effects
		// (Sections 13.9, 14.12).
		if def.Effect != "read_only" && def.Idempotency != "pure" && strings.TrimSpace(def.DryRun) == "" {
			fail("dry_run:"+def.Name, "effectful tools must declare dry_run")
		} else if def.Effect != "read_only" {
			pass("dry_run:" + def.Name)
		}
	}
	if dup {
		fail("names_unique", "duplicate tool names in manifest")
	} else {
		pass("names_unique")
	}
	if len(probes) == 0 {
		fail("probes", "the suite needs at least one behavioral probe per route")
		return report
	}
	byTool := map[string][]ConformanceProbe{}
	for _, p := range probes {
		byTool[p.Tool] = append(byTool[p.Tool], p)
	}
	for tool := range byTool {
		if !seen[tool] {
			fail("probe:"+tool, "probe targets a tool outside the manifest")
		}
	}
	// 2. Behavioral probes: same key twice, cancel promptness, budget.
	for _, p := range probes {
		name := "probe:" + p.Tool
		args := p.Args
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		key := "conformance-" + ext.Manifest.ID + "-" + p.Tool
		first, err := exec.Execute(ctx, p.Tool, args, ExecuteOptions{IdempotencyKey: key})
		if err != nil {
			fail(name, fmt.Sprintf("first invoke error: %v", err))
			continue
		}
		if first.Status == StatusInputError {
			fail(name, fmt.Sprintf("probe args rejected: %s", first.Output))
			continue
		}
		second, err := exec.Execute(ctx, p.Tool, args, ExecuteOptions{IdempotencyKey: key})
		if err != nil {
			fail(name, fmt.Sprintf("second invoke error: %v", err))
			continue
		}
		// Idempotency with the same key: the harness resends the key
		// after a crash, so the repeat must succeed with the same
		// outcome class (Section 14.1).
		sameOutcome := first.Status == second.Status &&
			(first.Status != StatusOK || first.Output == second.Output)
		if def, gerr := reg.Get(p.Tool); gerr == nil {
			switch def.Definition().Idempotency {
			case types.IdemPure, types.IdemIdempotent, types.IdemKeyed:
				if !sameOutcome {
					fail(name, fmt.Sprintf("same key gave different outcomes: %q vs %q", first.Status, second.Status))
					continue
				}
			}
			// Output inside budget: the pipeline truncates, and the
			// full text stays reachable by blob ref.
			if int64(len(second.Output)) > def.Definition().OutputBudget && !second.Truncated {
				fail(name, "output exceeds output_budget without truncation")
				continue
			}
		}
		if second.Status != StatusOK && second.Status != StatusTimeout && second.Status != StatusCanceled {
			fail(name, fmt.Sprintf("repeat invoke status %q: %s", second.Status, second.Output))
			continue
		}
		pass(name)
		// 3. Cancel honored: a canceled context must return promptly
		// (within 5 s) instead of hanging past the declared class.
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		done := make(chan Result, 1)
		safego.Go("tools/conformance-cancel-probe", nil, func() {
			res, _ := exec.Execute(cctx, p.Tool, args, ExecuteOptions{IdempotencyKey: key + "-cancel"})
			done <- res
		})
		select {
		case <-done:
			pass("cancel:" + p.Tool)
		case <-time.After(3 * time.Second):
			fail("cancel:"+p.Tool, "tool ignored a canceled context for 3s")
		case <-ctx.Done():
			pass("cancel:" + p.Tool)
		}
	}
	return report
}

// FormatReport renders the suite outcome for excalibur ext test.
func FormatReport(r ConformanceReport) string {
	var b strings.Builder
	status := "PASS"
	if !r.Passed {
		status = "FAIL"
	}
	fmt.Fprintf(&b, "extension %s: %s (%d checks)\n", r.Extension, status, len(r.Checks))
	names := make([]ConformanceCheck, len(r.Checks))
	copy(names, r.Checks)
	sort.Slice(names, func(i, j int) bool { return names[i].Name < names[j].Name })
	for _, c := range names {
		mark := "ok"
		if !c.Passed {
			mark = "FAIL"
		}
		if c.Detail != "" {
			fmt.Fprintf(&b, "  [%s] %s: %s\n", mark, c.Name, c.Detail)
		} else {
			fmt.Fprintf(&b, "  [%s] %s\n", mark, c.Name)
		}
	}
	return b.String()
}
