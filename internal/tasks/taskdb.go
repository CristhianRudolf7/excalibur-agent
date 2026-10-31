// The task_db tool: one tool with grouped operations executed in a
// single transaction (Section 8.10).
//
// Operations cover projects (register, update, archive), tasks,
// plans, dependencies, milestones, requirements, criteria, checks,
// evidence, assumptions, decisions and the logbook:
//
//	{"ops": [
//	  {"op": "create_task", "scope": "prj_01J…", "title": "Export to PDF", "requirements": ["R4"]},
//	  {"op": "add_criterion", "requirement": "R4", "statement": "The PDF keeps headers", "method": "test"},
//	  {"op": "record_evidence", "check": "chk_01J…", "result": "pass", "tool_call": "tc_123"},
//	  {"op": "query", "entity": "criteria", "where": {"status": ["failed", "unverified"]}, "fields": ["id", "statement", "status"], "limit": 20}
//	]}
//
// Queries return by default at most 20 rows and about 2,000 tokens,
// with the requested fields and a has-more indicator. Invalid
// operations fail with concrete errors (which reference is missing,
// which graph rule is violated) without applying the batch: the
// batch validates first and applies only when every operation is
// valid. The SQL adapter must run both phases in one porter
// transaction; MemoryStore applies them under one lock.
//
// Operations that modify tasks, plans, requirements or criteria
// carry the version the agent read; a mismatch rejects the whole
// batch with the current row (Section 8.10, optimistic concurrency).
package tasks

import (
	"fmt"
	"maps"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Query limits of Section 8.10: defaults, calibratable (Appendix B).
const (
	// DefaultQueryRows caps rows per query.
	DefaultQueryRows = 20
	// DefaultQueryTokens caps the token estimate per query response.
	DefaultQueryTokens = 2000
)

// OpCall is one grouped operation: a name plus its parameters, as
// decoded from the tool JSON.
type OpCall struct {
	Op     string
	Params map[string]any
}

// OpResult is the outcome of one applied operation.
type OpResult struct {
	Op      string           `json:"op"`
	ID      string           `json:"id,omitempty"`
	Code    string           `json:"code,omitempty"`
	Rows    []map[string]any `json:"rows,omitempty"`
	HasMore bool             `json:"has_more,omitempty"`
	Tokens  int              `json:"tokens,omitempty"`
}

// BatchError rejects a whole batch: nothing was applied. Index is
// the position of the failing operation.
type BatchError struct {
	Index  int
	Op     string
	Reason error
}

// Error implements error.
func (e *BatchError) Error() string {
	return fmt.Sprintf("tasks: op %d (%s): %v", e.Index, e.Op, e.Reason)
}

// Unwrap returns the concrete cause.
func (e *BatchError) Unwrap() error { return e.Reason }

// ApplyBatch validates every operation and applies the batch only
// when all are valid. Validation and application run against a
// private copy of the store; the live store is swapped in under one
// lock only when every operation succeeds, so a rejected batch
// leaves the database unchanged. (The SQL adapter must run both
// phases in one porter transaction.)
func ApplyBatch(store *MemoryStore, scope types.Scope, ops []OpCall) ([]OpResult, error) {
	plan := make([]pendingOp, 0, len(ops))
	for i, call := range ops {
		build, err := buildOp(scope, call)
		if err != nil {
			return nil, &BatchError{Index: i, Op: call.Op, Reason: err}
		}
		plan = append(plan, build)
	}
	work := store.clone()
	var out []OpResult
	for i, s := range plan {
		// Check and apply interleave on the private copy: a later
		// operation sees the rows an earlier one created, and a
		// rejected batch discards the copy, so the live store is
		// still all-or-nothing from the outside.
		if _, err := s.check(work); err != nil {
			return nil, &BatchError{Index: i, Op: ops[i].Op, Reason: err}
		}
		res, err := s.apply(work)
		if err != nil {
			return nil, &BatchError{Index: i, Op: ops[i].Op, Reason: err}
		}
		out = append(out, res)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.restore(work)
	return out, nil
}

// pendingOp is one parsed operation: check validates against the
// batch copy without mutating, apply performs the mutation.
type pendingOp struct {
	check func(db *MemoryStore) (OpResult, error)
	apply func(db *MemoryStore) (OpResult, error)
}

// buildOp parses one call into its check/apply closures. Reference
// resolution (ids or short codes) happens inside the closures
// against the batch copy, so later operations can name rows earlier
// operations created.
func buildOp(scope types.Scope, call OpCall) (pendingOp, error) {
	p := call.Params
	switch call.Op {
	case "register_project":
		name, _ := p["name"].(string)
		if name == "" {
			return pendingOp{}, fmt.Errorf("register_project: missing name")
		}
		roots := strSlice(p["roots"])
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) { return OpResult{}, nil },
			apply: func(db *MemoryStore) (OpResult, error) {
				id := types.NewProjectID()
				now := time.Now().UTC()
				proj := Project{ID: id, Name: name, Roots: roots, CreatedAt: now, UpdatedAt: now}
				if alias, _ := p["alias"].(string); alias != "" {
					proj.Alias = alias
				}
				if err := db.registerProjectLocked(proj); err != nil {
					return OpResult{}, err
				}
				return OpResult{Op: call.Op, ID: string(id)}, nil
			},
		}, nil
	case "create_task":
		title, _ := p["title"].(string)
		if title == "" {
			return pendingOp{}, fmt.Errorf("create_task: missing title")
		}
		level := types.TaskLevel(strOf(p["level"], string(types.TaskN1)))
		reqCodes := strSlice(p["requirements"])
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				for _, rc := range reqCodes {
					if _, err := db.requirementByCodeLocked(scope, types.RequirementRef(rc)); err != nil {
						return OpResult{}, err
					}
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				code := db.nextTaskCodeLocked(scope)
				id := types.NewTaskID()
				now := time.Now().UTC()
				t := types.Task{
					ID: id, Code: code, Scope: scope, Level: level,
					Request:   types.RequestFile{Original: title, Interpretation: title},
					State:     types.TaskOpen,
					Version:   1,
					CreatedAt: now, UpdatedAt: now,
				}
				if err := db.createTaskLocked(t); err != nil {
					return OpResult{}, err
				}
				for _, rc := range reqCodes {
					req, err := db.requirementByCodeLocked(scope, types.RequirementRef(rc))
					if err != nil {
						return OpResult{}, err
					}
					db.taskReqs[t.ID] = append(db.taskReqs[t.ID], req.ID)
				}
				return OpResult{Op: call.Op, ID: string(id), Code: string(code)}, nil
			},
		}, nil
	case "update_task":
		ver, err := versionOf(p)
		if err != nil {
			return pendingOp{}, err
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				id, err := taskRefOf(db, scope, p)
				if err != nil {
					return OpResult{}, err
				}
				cur, err := db.getTaskLocked(id)
				if err != nil {
					return OpResult{}, err
				}
				if cur.Version != ver {
					return OpResult{}, &VersionConflict{Entity: "task", ID: string(id), Have: cur.Version, Want: ver, Current: cur}
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				id, err := taskRefOf(db, scope, p)
				if err != nil {
					return OpResult{}, err
				}
				cur, _ := db.getTaskLocked(id)
				if title, ok := p["title"].(string); ok && title != "" {
					cur.Request.Interpretation = title
				}
				cur.Version++
				cur.UpdatedAt = time.Now().UTC()
				db.tasks[id] = cur
				return OpResult{Op: call.Op, ID: string(id), Code: string(cur.Code)}, nil
			},
		}, nil
	case "create_plan":
		typ := types.PlanType(strOf(p["type"], string(types.PlanMacro)))
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) { return OpResult{}, nil },
			apply: func(db *MemoryStore) (OpResult, error) {
				id := "pln_" + types.NewULID()
				now := time.Now().UTC()
				plan := Plan{ID: id, Scope: scope, Type: typ, Version: 1, State: PlanDraft, CreatedAt: now, UpdatedAt: now}
				if tid, ok := p["task"].(string); ok && tid != "" {
					plan.TaskID = types.TaskID(tid)
				}
				db.plans[id] = append(db.plans[id], plan)
				return OpResult{Op: call.Op, ID: id}, nil
			},
		}, nil
	case "add_plan_item":
		planID, _ := p["plan"].(string)
		title, _ := p["title"].(string)
		if planID == "" || title == "" {
			return pendingOp{}, fmt.Errorf("add_plan_item: missing plan or title")
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				if _, err := db.getPlanLocked(planID); err != nil {
					return OpResult{}, err
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				id := "pli_" + types.NewULID()
				now := time.Now().UTC()
				it := PlanItem{ID: id, PlanID: planID, Title: title, State: types.PlanItemPending, Version: 1, CreatedAt: now, UpdatedAt: now}
				if code, ok := p["code"].(string); ok {
					it.Code = code
				} else {
					it.Code = fmt.Sprintf("S%d", len(db.itemPlans[planID])+1)
				}
				if d, ok := p["detail"].(string); ok {
					it.Detail = d
				}
				db.items[id] = it
				db.itemPlans[planID] = append(db.itemPlans[planID], id)
				return OpResult{Op: call.Op, ID: id, Code: it.Code}, nil
			},
		}, nil
	case "update_plan_item":
		id, _ := p["id"].(string)
		if id == "" {
			return pendingOp{}, fmt.Errorf("update_plan_item: missing id")
		}
		ver, err := versionOf(p)
		if err != nil {
			return pendingOp{}, err
		}
		state, _ := p["state"].(string)
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				cur, err := db.getPlanItemLocked(id)
				if err != nil {
					return OpResult{}, err
				}
				if cur.Version != ver {
					return OpResult{}, &VersionConflict{Entity: "plan_item", ID: id, Have: cur.Version, Want: ver, Current: cur}
				}
				if state != "" {
					if err := validPlanItemState(state); err != nil {
						return OpResult{}, err
					}
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				cur, _ := db.getPlanItemLocked(id)
				if state != "" {
					cur.State = types.PlanItemState(state)
				}
				if title, ok := p["title"].(string); ok && title != "" {
					cur.Title = title
				}
				cur.Version++
				cur.UpdatedAt = time.Now().UTC()
				db.items[id] = cur
				return OpResult{Op: call.Op, ID: id}, nil
			},
		}, nil
	case "add_dependency":
		planID, _ := p["plan"].(string)
		from, _ := p["from"].(string)
		to, _ := p["to"].(string)
		dtype := types.DependencyType(strOf(p["type"], string(types.DependencyBlocks)))
		if planID == "" || from == "" || to == "" {
			return pendingOp{}, fmt.Errorf("add_dependency: missing plan, from or to")
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				if _, err := db.getPlanItemLocked(from); err != nil {
					return OpResult{}, fmt.Errorf("add_dependency: %w", err)
				}
				if _, err := db.getPlanItemLocked(to); err != nil {
					return OpResult{}, fmt.Errorf("add_dependency: %w", err)
				}
				var nodes []string
				nodes = append(nodes, db.itemPlans[planID]...)
				if WouldCycle(nodes, db.deps[planID], from, to) {
					return OpResult{}, fmt.Errorf("add_dependency: edge %q -> %q would close a cycle", from, to)
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				db.deps[planID] = append(db.deps[planID], Dependency{From: from, To: to, Type: dtype})
				return OpResult{Op: call.Op}, nil
			},
		}, nil
	case "add_milestone":
		planID, _ := p["plan"].(string)
		title, _ := p["title"].(string)
		if planID == "" || title == "" {
			return pendingOp{}, fmt.Errorf("add_milestone: missing plan or title")
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				if _, err := db.getPlanLocked(planID); err != nil {
					return OpResult{}, err
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				code := db.nextMilestoneCodeLocked(planID)
				m := Milestone{ID: "mls_" + types.NewULID(), Code: code, PlanID: planID, Title: title, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
				db.milestones[planID] = append(db.milestones[planID], m)
				return OpResult{Op: call.Op, ID: m.ID, Code: string(code)}, nil
			},
		}, nil
	case "add_requirement":
		text, _ := p["text"].(string)
		if text == "" {
			return pendingOp{}, fmt.Errorf("add_requirement: missing text")
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) { return OpResult{}, nil },
			apply: func(db *MemoryStore) (OpResult, error) {
				code := db.nextRequirementCodeLocked(scope)
				id := types.NewRequirementID()
				now := time.Now().UTC()
				r := types.Requirement{ID: id, Code: code, Scope: scope, Text: text, State: types.RequirementProposed, Version: 1, CreatedAt: now, UpdatedAt: now}
				if t, ok := p["type"].(string); ok && t != "" {
					r.Type = types.RequirementType(t)
				}
				if pr, ok := p["priority"].(string); ok && pr != "" {
					r.Priority = types.Priority(pr)
				}
				if err := db.addRequirementLocked(r); err != nil {
					return OpResult{}, err
				}
				return OpResult{Op: call.Op, ID: string(id), Code: string(code)}, nil
			},
		}, nil
	case "update_requirement":
		ver, err := versionOf(p)
		if err != nil {
			return pendingOp{}, err
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				id, err := requirementRefOf(db, scope, p)
				if err != nil {
					return OpResult{}, err
				}
				cur, err := db.getRequirementLocked(id)
				if err != nil {
					return OpResult{}, err
				}
				if cur.Version != ver {
					return OpResult{}, &VersionConflict{Entity: "requirement", ID: string(id), Have: cur.Version, Want: ver, Current: cur}
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				id, err := requirementRefOf(db, scope, p)
				if err != nil {
					return OpResult{}, err
				}
				cur, _ := db.getRequirementLocked(id)
				if text, ok := p["text"].(string); ok && text != "" {
					cur.Text = text
				}
				if st, ok := p["state"].(string); ok && st != "" {
					cur.State = types.RequirementState(st)
				}
				cur.Version++
				cur.UpdatedAt = time.Now().UTC()
				db.requirements[id] = cur
				return OpResult{Op: call.Op, ID: string(id)}, nil
			},
		}, nil
	case "add_criterion":
		statement, _ := p["statement"].(string)
		method, _ := p["method"].(string)
		if statement == "" || method == "" {
			return pendingOp{}, fmt.Errorf("add_criterion: missing statement or method")
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				if _, err := requirementRefOf(db, scope, p); err != nil {
					return OpResult{}, err
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				reqID, err := requirementRefOf(db, scope, p)
				if err != nil {
					return OpResult{}, err
				}
				reqNum := db.reqNumbers[reqID]
				code := db.nextCriterionCodeLocked(reqID, reqNum)
				id := types.NewCriterionID()
				now := time.Now().UTC()
				c := types.Criterion{
					ID: id, Code: code, RequirementID: reqID,
					Statement: statement, Method: types.CriterionMethod(method),
					Expected:   strOf(p["expected"], ""),
					ImpactArea: strSlice(p["impact_area"]),
					CreatedAt:  now, UpdatedAt: now,
				}
				if err := db.addCriterionLocked(c); err != nil {
					return OpResult{}, err
				}
				return OpResult{Op: call.Op, ID: string(id), Code: code.String()}, nil
			},
		}, nil
	case "update_criterion":
		ver, err := versionOf(p)
		if err != nil {
			return pendingOp{}, err
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				id, err := criterionRefOf(db, scope, p)
				if err != nil {
					return OpResult{}, err
				}
				cur, err := db.getCriterionLocked(id)
				if err != nil {
					return OpResult{}, err
				}
				have := db.criterionVers[cur.ID]
				if have == 0 {
					have = 1
				}
				if have != ver {
					return OpResult{}, &VersionConflict{Entity: "criterion", ID: string(id), Have: have, Want: ver, Current: cur}
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				id, err := criterionRefOf(db, scope, p)
				if err != nil {
					return OpResult{}, err
				}
				cur, _ := db.getCriterionLocked(id)
				if s, ok := p["statement"].(string); ok && s != "" {
					cur.Statement = s
				}
				if ia, ok := p["impact_area"]; ok {
					cur.ImpactArea = strSlice(ia)
				}
				db.criterionVers[cur.ID]++
				cur.UpdatedAt = time.Now().UTC()
				db.criteria[id] = cur
				return OpResult{Op: call.Op, ID: string(id)}, nil
			},
		}, nil
	case "add_check":
		def, _ := p["definition"].(string)
		if def == "" {
			return pendingOp{}, fmt.Errorf("add_check: missing definition")
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				if _, err := criterionRefOf(db, scope, p); err != nil {
					return OpResult{}, err
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				critID, err := criterionRefOf(db, scope, p)
				if err != nil {
					return OpResult{}, err
				}
				id := types.NewCheckID()
				now := time.Now().UTC()
				chk := types.Check{ID: id, CriterionID: critID, Definition: def, Environment: strOf(p["environment"], ""), CreatedAt: now, UpdatedAt: now}
				db.checks[id] = chk
				return OpResult{Op: call.Op, ID: string(id)}, nil
			},
		}, nil
	case "record_evidence":
		chkStr, _ := p["check"].(string)
		result, _ := p["result"].(string)
		if chkStr == "" || result == "" {
			return pendingOp{}, fmt.Errorf("record_evidence: missing check or result")
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				if _, err := db.getCheckLocked(types.CheckID(chkStr)); err != nil {
					return OpResult{}, err
				}
				switch types.EvidenceResult(result) {
				case types.EvidencePass, types.EvidenceFail, types.EvidenceError:
				default:
					return OpResult{}, fmt.Errorf("record_evidence: bad result %q: want pass, fail or error", result)
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				id := types.NewEvidenceID()
				ev := types.Evidence{
					ID: id, CheckID: types.CheckID(chkStr), Result: types.EvidenceResult(result),
					OutputBlob:   strOf(p["output_blob"], ""),
					CodeVersion:  strOf(p["code_version"], ""),
					ToolCallID:   strOf(p["tool_call"], ""),
					Fingerprints: strMap(p["fingerprints"]),
					CreatedAt:    time.Now().UTC(),
				}
				if rid, ok := p["run"].(string); ok && rid != "" {
					ev.RunID = types.RunID(rid)
				}
				revSeen := intOf(p["instruction_rev_seen"], db.instrRevs[scope.String()])
				db.evidence[ev.CheckID] = append(db.evidence[ev.CheckID], EvidenceRecord{Evidence: ev, InstructionRevSeen: revSeen})
				return OpResult{Op: call.Op, ID: string(id)}, nil
			},
		}, nil
	case "add_assumption", "add_decision", "append_log":
		text := strOf(p["text"], strOf(p["statement"], ""))
		if text == "" {
			return pendingOp{}, fmt.Errorf("%s: missing text", call.Op)
		}
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) { return OpResult{}, nil },
			apply: func(db *MemoryStore) (OpResult, error) {
				now := time.Now().UTC()
				switch call.Op {
				case "add_assumption":
					a := types.Assumption{ID: "asm_" + types.NewULID(), Text: text, Reason: strOf(p["reason"], ""), CreatedAt: now, UpdatedAt: now}
					db.assumptions[a.ID] = a
					return OpResult{Op: call.Op, ID: a.ID}, nil
				case "add_decision":
					d := types.Decision{ID: "dec_" + types.NewULID(), Statement: text, Reason: strOf(p["reason"], ""), Alternatives: strSlice(p["alternatives"]), CreatedAt: now, UpdatedAt: now}
					db.decisions[d.ID] = d
					return OpResult{Op: call.Op, ID: d.ID}, nil
				default:
					var proj types.ProjectID
					if ps, ok := p["project"].(string); ok && ps != "" {
						proj = types.ProjectID(ps)
					}
					e := ProjectLogEntry{ID: "log_" + types.NewULID(), ProjectID: proj, Kind: strOf(p["kind"], "note"), Text: text, Refs: strSlice(p["refs"]), CreatedAt: now}
					db.logs[proj] = append(db.logs[proj], e)
					return OpResult{Op: call.Op, ID: e.ID}, nil
				}
			},
		}, nil
	case "query":
		entity, _ := p["entity"].(string)
		return pendingOp{
			check: func(db *MemoryStore) (OpResult, error) {
				if !validQueryEntity(entity) {
					return OpResult{}, fmt.Errorf("query: unknown entity %q", entity)
				}
				return OpResult{}, nil
			},
			apply: func(db *MemoryStore) (OpResult, error) {
				return runQueryLocked(db, scope, p), nil
			},
		}, nil
	default:
		return pendingOp{}, fmt.Errorf("unknown op %q", call.Op)
	}
}

func validQueryEntity(e string) bool {
	switch e {
	case "tasks", "requirements", "criteria", "evidence", "plans", "plan_items", "milestones", "assumptions", "decisions", "log":
		return true
	default:
		return false
	}
}

// runQueryLocked executes a query op against the locked store. Rows
// carry only the requested fields, the row cap defaults to 20, and
// the token estimate (~4 chars per token) caps around 2000 tokens;
// both caps set HasMore when rows are left out.
func runQueryLocked(store *MemoryStore, scope types.Scope, p map[string]any) OpResult {
	limit := intOf(p["limit"], DefaultQueryRows)
	if limit <= 0 {
		limit = DefaultQueryRows
	}
	var fields []string
	if f, ok := p["fields"].([]string); ok {
		fields = f
	} else {
		fields = strSlice(p["fields"])
	}
	where := map[string][]string{}
	if w, ok := p["where"].(map[string]any); ok {
		for k, v := range w {
			where[k] = strSlice(v)
		}
	}
	entity, _ := p["entity"].(string)
	var rows []map[string]any
	switch entity {
	case "criteria":
		rows = queryCriteriaLocked(store, scope, where)
	case "tasks":
		rows = queryTasksLocked(store, scope, where)
	case "requirements":
		rows = queryRequirementsLocked(store, scope, where)
	case "evidence":
		rows = queryEvidenceLocked(store, scope, where)
	default:
		rows = []map[string]any{}
	}
	rows = projectFields(rows, fields)
	hasMore := false
	if len(rows) > limit {
		rows = rows[:limit]
		hasMore = true
	}
	tokens := 0
	kept := rows
	for i, r := range rows {
		tokens += rowTokens(r)
		if tokens > DefaultQueryTokens && i+1 < len(rows) {
			kept = rows[:i+1]
			hasMore = true
			break
		}
	}
	return OpResult{Op: "query", Rows: kept, HasMore: hasMore, Tokens: tokens}
}

func queryCriteriaLocked(store *MemoryStore, scope types.Scope, where map[string][]string) []map[string]any {
	var out []map[string]any
	statuses, hasStatus := where["status"]
	reqs := store.listRequirementsLocked(scope)
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Code < reqs[j].Code })
	for _, req := range reqs {
		crits := store.listCriteriaLocked(req.ID)
		sort.Slice(crits, func(i, j int) bool { return crits[i].Code.String() < crits[j].Code.String() })
		for _, c := range crits {
			st := statusOfLocked(store, scope, c)
			if hasStatus && !contains(statuses, statusName(st)) {
				continue
			}
			out = append(out, map[string]any{
				"id": c.ID, "code": c.Code.String(), "statement": c.Statement,
				"method": c.Method, "status": statusName(st),
				"requirement": req.Code,
			})
		}
	}
	return out
}

func queryTasksLocked(store *MemoryStore, scope types.Scope, where map[string][]string) []map[string]any {
	var out []map[string]any
	tasks := store.listTasksLocked(scope)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Code < tasks[j].Code })
	statuses, hasStatus := where["status"]
	for _, t := range tasks {
		if hasStatus && !contains(statuses, string(t.State)) {
			continue
		}
		out = append(out, map[string]any{
			"id": t.ID, "code": t.Code, "title": t.Request.Interpretation,
			"level": t.Level, "status": t.State,
		})
	}
	return out
}

func queryRequirementsLocked(store *MemoryStore, scope types.Scope, where map[string][]string) []map[string]any {
	var out []map[string]any
	reqs := store.listRequirementsLocked(scope)
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Code < reqs[j].Code })
	statuses, hasStatus := where["status"]
	for _, r := range reqs {
		if hasStatus && !contains(statuses, string(r.State)) {
			continue
		}
		out = append(out, map[string]any{
			"id": r.ID, "code": r.Code, "text": r.Text,
			"priority": r.Priority, "status": r.State,
		})
	}
	return out
}

func queryEvidenceLocked(store *MemoryStore, scope types.Scope, where map[string][]string) []map[string]any {
	var out []map[string]any
	checks, hasCheck := where["check"]
	for chkID, recs := range store.evidence {
		if hasCheck && !contains(checks, string(chkID)) {
			continue
		}
		for _, rec := range recs {
			out = append(out, map[string]any{
				"id": rec.Evidence.ID, "check": chkID, "result": rec.Evidence.Result,
				"code_version": rec.Evidence.CodeVersion, "tool_call": rec.Evidence.ToolCallID,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(types.EvidenceID) < out[j]["id"].(types.EvidenceID) })
	return out
}

// statusOfLocked resolves one criterion to its queryable status,
// including the stale marker freshness computes.
func statusOfLocked(store *MemoryStore, scope types.Scope, c types.Criterion) CriterionStatus {
	var latest *EvidenceRecord
	for _, chk := range store.listChecksLocked(c.ID) {
		for _, rec := range store.evidence[chk.ID] {
			r := rec
			if latest == nil || r.Evidence.CreatedAt.After(latest.Evidence.CreatedAt) {
				latest = &r
			}
		}
	}
	cur := map[string]string{}
	for _, p := range c.ImpactArea {
		if fp, ok := store.filePrints[p]; ok {
			cur[p] = fp
		}
	}
	return ComputeStatus(c, latest, cur, store.instrRevs[scope.String()])
}

func statusName(st CriterionStatus) string {
	if st.Stale {
		return "stale"
	}
	switch st.State {
	case types.CriterionSatisfied:
		return "satisfied"
	case types.CriterionFailed:
		return "failed"
	case types.CriterionNotApplicable:
		return "not_applicable"
	default:
		if !st.HasEvidence {
			return "unverified"
		}
		return "unverified"
	}
}

// projectFields keeps only the requested fields; an empty list keeps
// a small default per entity shape (id, code/title/text, status).
func projectFields(rows []map[string]any, fields []string) []map[string]any {
	if len(fields) == 0 {
		return rows
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		m := map[string]any{}
		for _, f := range fields {
			if v, ok := r[f]; ok {
				m[f] = v
			}
		}
		out = append(out, m)
	}
	return out
}

// rowTokens estimates response tokens at four characters each.
func rowTokens(r map[string]any) int {
	n := 0
	for k, v := range r {
		n += len(k) + len(fieldString(v))
	}
	return n/4 + 1
}

// fieldString renders a field value for token estimation.
func fieldString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// Locked helpers: they never lock; ApplyBatch runs the whole batch
// against a private copy and swaps it in under one lock, while the
// plain PlanningStore methods lock per call.
func (m *MemoryStore) registerProjectLocked(p Project) error {
	if _, ok := m.projects[p.ID]; ok {
		return fmt.Errorf("tasks: project %q already registered", p.ID)
	}
	m.projects[p.ID] = p
	return nil
}

func (m *MemoryStore) getTaskLocked(id types.TaskID) (types.Task, error) {
	t, ok := m.tasks[id]
	if !ok {
		return types.Task{}, fmt.Errorf("tasks: unknown task %q", id)
	}
	return t, nil
}

func (m *MemoryStore) taskByCodeLocked(scope types.Scope, code types.TaskRef) (types.Task, error) {
	id, ok := m.taskCodes[scope.String()+"\x00"+string(code)]
	if !ok {
		return types.Task{}, fmt.Errorf("tasks: unknown task %q in scope %q", code, scope.String())
	}
	return m.tasks[id], nil
}

func (m *MemoryStore) criterionByCodeLocked(scope types.Scope, code string) (types.Criterion, error) {
	ref, err := types.ParseCriterionRef(code)
	if err != nil {
		return types.Criterion{}, fmt.Errorf("tasks: %w", err)
	}
	reqCode := string(types.FormatRequirementRef(ref.Requirement))
	reqID, ok := m.reqCodes[scope.String()+"\x00"+reqCode]
	if !ok {
		return types.Criterion{}, fmt.Errorf("tasks: unknown requirement %q in scope %q", reqCode, scope.String())
	}
	for _, c := range m.criteria {
		if c.RequirementID == reqID && c.Code == ref {
			return c, nil
		}
	}
	return types.Criterion{}, fmt.Errorf("tasks: unknown criterion %q in scope %q", code, scope.String())
}

func (m *MemoryStore) createTaskLocked(t types.Task) error {
	if _, ok := m.tasks[t.ID]; ok {
		return fmt.Errorf("tasks: task %q already exists", t.ID)
	}
	m.tasks[t.ID] = t
	m.taskCodes[t.Scope.String()+"\x00"+string(t.Code)] = t.ID
	return nil
}

func (m *MemoryStore) nextTaskCodeLocked(scope types.Scope) types.TaskRef {
	m.taskNums[scope.String()]++
	return types.FormatTaskRef(m.taskNums[scope.String()])
}

func (m *MemoryStore) nextRequirementCodeLocked(scope types.Scope) types.RequirementRef {
	m.reqNums[scope.String()]++
	return types.FormatRequirementRef(m.reqNums[scope.String()])
}

func (m *MemoryStore) nextCriterionCodeLocked(req types.RequirementID, reqNum int) types.CriterionRef {
	m.criterionNums[req]++
	return types.CriterionRef{Requirement: reqNum, Criterion: m.criterionNums[req]}
}

func (m *MemoryStore) nextMilestoneCodeLocked(planID string) types.MilestoneRef {
	m.milestoneNums[planID]++
	return types.FormatMilestoneRef(m.milestoneNums[planID])
}

func (m *MemoryStore) requirementByCodeLocked(scope types.Scope, code types.RequirementRef) (types.Requirement, error) {
	id, ok := m.reqCodes[scope.String()+"\x00"+string(code)]
	if !ok {
		return types.Requirement{}, fmt.Errorf("tasks: unknown requirement %q in scope %q", code, scope.String())
	}
	return m.requirements[id], nil
}

func (m *MemoryStore) getRequirementLocked(id types.RequirementID) (types.Requirement, error) {
	r, ok := m.requirements[id]
	if !ok {
		return types.Requirement{}, fmt.Errorf("tasks: unknown requirement %q", id)
	}
	return r, nil
}

func (m *MemoryStore) addRequirementLocked(r types.Requirement) error {
	if _, ok := m.requirements[r.ID]; ok {
		return fmt.Errorf("tasks: requirement %q already exists", r.ID)
	}
	m.requirements[r.ID] = r
	m.reqCodes[r.Scope.String()+"\x00"+string(r.Code)] = r.ID
	if _, ok := m.reqNumbers[r.ID]; !ok {
		m.reqNumbers[r.ID] = r.Code.Number()
	}
	return nil
}

func (m *MemoryStore) getCriterionLocked(id types.CriterionID) (types.Criterion, error) {
	c, ok := m.criteria[id]
	if !ok {
		return types.Criterion{}, fmt.Errorf("tasks: unknown criterion %q", id)
	}
	return c, nil
}

func (m *MemoryStore) addCriterionLocked(c types.Criterion) error {
	if _, ok := m.criteria[c.ID]; ok {
		return fmt.Errorf("tasks: criterion %q already exists", c.ID)
	}
	m.criteria[c.ID] = c
	m.criterionVers[c.ID] = 1
	return nil
}

func (m *MemoryStore) getCheckLocked(id types.CheckID) (types.Check, error) {
	c, ok := m.checks[id]
	if !ok {
		return types.Check{}, fmt.Errorf("tasks: unknown check %q", id)
	}
	return c, nil
}

func (m *MemoryStore) getPlanLocked(id string) (Plan, error) {
	vers, ok := m.plans[id]
	if !ok || len(vers) == 0 {
		return Plan{}, fmt.Errorf("tasks: unknown plan %q", id)
	}
	return vers[len(vers)-1], nil
}

func (m *MemoryStore) getPlanItemLocked(id string) (PlanItem, error) {
	it, ok := m.items[id]
	if !ok {
		return PlanItem{}, fmt.Errorf("tasks: unknown plan item %q", id)
	}
	return it, nil
}

func (m *MemoryStore) listRequirementsLocked(scope types.Scope) []types.Requirement {
	var out []types.Requirement
	for _, r := range m.requirements {
		if r.Scope == scope {
			out = append(out, r)
		}
	}
	return out
}

func (m *MemoryStore) listCriteriaLocked(req types.RequirementID) []types.Criterion {
	var out []types.Criterion
	for _, c := range m.criteria {
		if c.RequirementID == req {
			out = append(out, c)
		}
	}
	return out
}

func (m *MemoryStore) listChecksLocked(c types.CriterionID) []types.Check {
	var out []types.Check
	for _, chk := range m.checks {
		if chk.CriterionID == c {
			out = append(out, chk)
		}
	}
	return out
}

func (m *MemoryStore) listTasksLocked(scope types.Scope) []types.Task {
	var out []types.Task
	for _, t := range m.tasks {
		if t.Scope == scope {
			out = append(out, t)
		}
	}
	return out
}

// Reference resolution for op parameters: ids or short codes,
// resolved against the batch copy so later operations can name rows
// earlier operations created.
func taskRefOf(db *MemoryStore, scope types.Scope, p map[string]any) (types.TaskID, error) {
	s, _ := p["task"].(string)
	if s == "" {
		return "", fmt.Errorf("missing task reference")
	}
	if err := types.TaskID(s).Validate(); err == nil {
		return types.TaskID(s), nil
	}
	code, err := types.ParseTaskRef(s)
	if err != nil {
		return "", fmt.Errorf("bad task reference %q: want tsk_ id or T<number>", s)
	}
	t, err := db.taskByCodeLocked(scope, code)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

func requirementRefOf(db *MemoryStore, scope types.Scope, p map[string]any) (types.RequirementID, error) {
	s, _ := p["requirement"].(string)
	if s == "" {
		return "", fmt.Errorf("missing requirement reference")
	}
	if err := types.RequirementID(s).Validate(); err == nil {
		return types.RequirementID(s), nil
	}
	code, err := types.ParseRequirementRef(s)
	if err != nil {
		return "", fmt.Errorf("bad requirement reference %q: want req_ id or R<number>", s)
	}
	r, err := db.requirementByCodeLocked(scope, code)
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

func criterionRefOf(db *MemoryStore, scope types.Scope, p map[string]any) (types.CriterionID, error) {
	s, _ := p["criterion"].(string)
	if s == "" {
		s, _ = p["check_criterion"].(string)
	}
	if s == "" {
		return "", fmt.Errorf("missing criterion reference")
	}
	if err := types.CriterionID(s).Validate(); err == nil {
		return types.CriterionID(s), nil
	}
	c, err := db.criterionByCodeLocked(scope, s)
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

func versionOf(p map[string]any) (int, error) {
	v, ok := p["version"]
	if !ok {
		return 0, fmt.Errorf("missing version (optimistic concurrency requires the version read)")
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("bad version %v: want a number", v)
	}
}

func validPlanItemState(s string) error {
	switch types.PlanItemState(s) {
	case types.PlanItemPending, types.PlanItemInProgress, types.PlanItemBlocked, types.PlanItemDone, types.PlanItemCanceled:
		return nil
	default:
		return fmt.Errorf("bad plan item state %q", s)
	}
}

func strOf(v any, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

func strSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	default:
		return nil
	}
}

func strMap(v any) map[string]string {
	out := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, e := range m {
			if s, ok := e.(string); ok {
				out[k] = s
			}
		}
	}
	return out
}

func intOf(v any, def int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return def
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// clone returns a private copy of the store for batch validation and
// application. Maps are copied; slices grow append-only during a
// batch, so sharing the backing arrays with the live store is safe
// until restore swaps the maps in.
func (m *MemoryStore) clone() *MemoryStore {
	c := NewMemoryStore()
	c.projects = maps.Clone(m.projects)
	c.tasks = maps.Clone(m.tasks)
	c.taskHistory = maps.Clone(m.taskHistory)
	c.plans = maps.Clone(m.plans)
	c.items = maps.Clone(m.items)
	c.itemPlans = maps.Clone(m.itemPlans)
	c.deps = maps.Clone(m.deps)
	c.milestones = maps.Clone(m.milestones)
	c.requirements = maps.Clone(m.requirements)
	c.criteria = maps.Clone(m.criteria)
	c.checks = maps.Clone(m.checks)
	c.evidence = maps.Clone(m.evidence)
	c.assumptions = maps.Clone(m.assumptions)
	c.decisions = maps.Clone(m.decisions)
	c.logs = maps.Clone(m.logs)
	c.taskReqs = maps.Clone(m.taskReqs)
	c.instrRevs = maps.Clone(m.instrRevs)
	c.filePrints = maps.Clone(m.filePrints)
	c.taskNums = maps.Clone(m.taskNums)
	c.reqNums = maps.Clone(m.reqNums)
	c.milestoneNums = maps.Clone(m.milestoneNums)
	c.criterionNums = maps.Clone(m.criterionNums)
	c.criterionVers = maps.Clone(m.criterionVers)
	c.taskCodes = maps.Clone(m.taskCodes)
	c.reqCodes = maps.Clone(m.reqCodes)
	c.reqNumbers = maps.Clone(m.reqNumbers)
	return c
}

// restore swaps a validated batch copy into the live store. The
// caller holds the store lock.
func (m *MemoryStore) restore(c *MemoryStore) {
	m.projects = c.projects
	m.tasks = c.tasks
	m.taskHistory = c.taskHistory
	m.plans = c.plans
	m.items = c.items
	m.itemPlans = c.itemPlans
	m.deps = c.deps
	m.milestones = c.milestones
	m.requirements = c.requirements
	m.criteria = c.criteria
	m.checks = c.checks
	m.evidence = c.evidence
	m.assumptions = c.assumptions
	m.decisions = c.decisions
	m.logs = c.logs
	m.taskReqs = c.taskReqs
	m.instrRevs = c.instrRevs
	m.filePrints = c.filePrints
	m.taskNums = c.taskNums
	m.reqNums = c.reqNums
	m.milestoneNums = c.milestoneNums
	m.criterionNums = c.criterionNums
	m.criterionVers = c.criterionVers
	m.taskCodes = c.taskCodes
	m.reqCodes = c.reqCodes
	m.reqNumbers = c.reqNumbers
}
