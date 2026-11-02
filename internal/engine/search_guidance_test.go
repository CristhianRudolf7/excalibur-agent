package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

func TestSearchGuidanceScopeAndSize(t *testing.T) {
	for _, tc := range []struct {
		name, enabled    string
		autonomous, want bool
	}{
		{"default", "", true, true}, {"disabled", "0", true, false}, {"assistant", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(searchGuidanceSwitch, tc.enabled)
			r := testRun(Profile{ExplicitFinish: tc.autonomous})
			b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: testSnapshotFunc()}
			_, compiled, err := b.BuildRequest(context.Background(), r, false)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(compiled.LayerText(ctxcompiler.LayerTask), searchGuidance); got != tc.want {
				t.Fatal("search guidance", got, tc.want)
			}
		})
	}
	if len(searchGuidance) > 160 {
		t.Fatal("search guidance grew", len(searchGuidance))
	}
	t.Logf("autonomous task guidance: +%d bytes", len(searchGuidance))
}
