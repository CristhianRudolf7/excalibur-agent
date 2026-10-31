package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	store "github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// TestShortCodesNeverReused deletes tasks and recreates them: codes
// keep counting up (Section 4.4).
func TestShortCodesNeverReused(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	scope := "project:prj_1"

	first, err := store.NextTaskCode(ctx, db, scope)
	if err != nil {
		t.Fatal(err)
	}
	if first != types.TaskRef("T1") {
		t.Fatalf("first code = %s, want T1", first)
	}
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO tasks(id,short_code,scope_id,level,status) VALUES('tsk_1',?,?,'N1','completed')`, string(first), scope)
		return err
	})
	// Delete the task; the counter must not move backwards.
	mustWrite(t, db, store.PriorityNormal, func(_ context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `DELETE FROM tasks WHERE id='tsk_1'`)
		return err
	})
	second, err := store.NextTaskCode(ctx, db, scope)
	if err != nil {
		t.Fatal(err)
	}
	if second != types.TaskRef("T2") {
		t.Fatalf("second code = %s, want T2", second)
	}
	// Requirements, milestones and criteria share the same guarantee.
	r1, err := store.NextRequirementCode(ctx, db, scope)
	if err != nil {
		t.Fatal(err)
	}
	if r1 != types.RequirementRef("R1") {
		t.Fatalf("req code = %s", r1)
	}
	m1, err := store.NextMilestoneCode(ctx, db, scope)
	if err != nil {
		t.Fatal(err)
	}
	if m1 != types.MilestoneRef("M1") {
		t.Fatalf("milestone code = %s", m1)
	}
	c1, err := store.NextCriterionCode(ctx, db, r1, "req_1")
	if err != nil {
		t.Fatal(err)
	}
	if c1.String() != "C1.1" {
		t.Fatalf("criterion code = %s", c1)
	}
	c2, err := store.NextCriterionCode(ctx, db, r1, "req_1")
	if err != nil {
		t.Fatal(err)
	}
	if c2.String() != "C1.2" {
		t.Fatalf("criterion code = %s", c2)
	}
}

// TestShortCodesConcurrent allocates from many goroutines: every code
// unique, the set gapless (Section 4.4, no coordination outside the
// porter).
func TestShortCodesConcurrent(t *testing.T) {
	db := openSchema(t, nil)
	ctx := context.Background()
	const n = 200
	got := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, err := store.NextTaskCode(ctx, db, "project:conc")
			if err != nil {
				t.Error(err)
				return
			}
			got <- string(code)
		}()
	}
	wg.Wait()
	close(got)
	seen := map[string]bool{}
	for c := range got {
		if seen[c] {
			t.Fatalf("duplicate code %s", c)
		}
		seen[c] = true
	}
	if len(seen) != n {
		t.Fatalf("%d codes, want %d", len(seen), n)
	}
	for i := 1; i <= n; i++ {
		if !seen[fmt.Sprintf("T%d", i)] {
			t.Fatalf("missing T%d", i)
		}
	}
}
