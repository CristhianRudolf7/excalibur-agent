package cli

import (
	"context"
	"encoding/json"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
	"testing"
)

func TestOfficeWriterProfileReachability(t *testing.T) {
	for _, profile := range []ProfileName{ProfileDefault, ProfileDelegate, ProfileEval} {
		t.Run(string(profile), func(t *testing.T) {
			emb, e := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{Workdir: t.TempDir()})
			if e != nil {
				t.Fatal(e)
			}
			runner := newProfileGatedRunner(profile, engine.NewRegistryRunner(emb.Registry, emb.Exec))
			args := json.RawMessage(`{"name":"document_create","arguments":{"path":"profile.docx","format":"docx","spec":{"title":"Profile test"}}}`)
			result, e := runner.Execute(context.Background(), "call_tool", args, tools.ExecuteOptions{})
			if profile == ProfileEval {
				if e == nil {
					t.Fatal("eval admitted Office writer")
				}
				return
			}
			if e != nil || result.Status != tools.StatusOK {
				t.Fatal(result, e)
			}
		})
	}
}

func TestEvalKeepsFrozenOfficeSkills(t *testing.T) {
	evalIx := builtinSkillsForProfile(ProfileEval)
	assistIx := builtinSkillsForProfile(ProfileDefault)
	for _, ix := range []*skills.Index{evalIx, assistIx} {
		if e := ix.Scan(); e != nil {
			t.Fatal(e)
		}
	}
	for _, name := range []string{"word-documents", "spreadsheets", "presentations", "pdf-documents"} {
		frozen, ok := evalIx.Get(name)
		if !ok {
			t.Fatalf("%s missing from eval", name)
		}
		current, ok := assistIx.Get(name)
		if !ok {
			t.Fatalf("%s missing from assistant", name)
		}
		if frozen.Hash == current.Hash {
			t.Fatalf("%s: eval must keep the frozen snapshot, not the assistant revision", name)
		}
	}
}
