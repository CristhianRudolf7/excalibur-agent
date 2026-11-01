package tasks

import (
	"strings"
	"testing"
)

func TestGeneralizationCoverageReport(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		pass        bool
	}{
		{"visible only", "FRESH_INPUTS: FAIL - only visible examples\nOUTPUT_CONTRACT: PASS\nPROPERTY: PASS - JSON parsed", false},
		{"wrong format", "FRESH_INPUTS: PASS - generated inputs\nOUTPUT_CONTRACT: FAIL - wrong field order\nPROPERTY: FAIL - ordering", false},
		{"proposed checks only", "CHECKS:\n- generate random data\nVERDICT: PASS", false},
		{"lowercase missing checklist", "fresh_inputs: pass\noutput_contract: pass", false},
		{"missing checklist", "FRESH_INPUTS: PASS\nOUTPUT_CONTRACT: PASS", false},
		{"generated and asserted", "FRESH_INPUTS: PASS - seed 7, sizes 0/4/11, independent sum\nPROPERTY: PASS - integer type assertion\nPROPERTY: PASS - field order assertion\nOUTPUT_CONTRACT: PASS", true},
		{"no applicable method", "FRESH_INPUTS: NA - prose request\nOUTPUT_CONTRACT: NA - no explicit format", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := GeneralizationPendings(tc.reply); (len(got) == 0) != tc.pass {
				t.Fatalf("pending = %v", got)
			}
		})
	}
}

func TestGeneralizationPromptSwitch(t *testing.T) {
	cfg := DefaultSelfVerifyConfig()
	small := VerifierSmallContext{Request: "Write a converter returning JSON with integer count, shape [2,3], fields a,b in order."}
	history := []HistoryTool{{Name: "exec", Args: strings.Repeat("x", 200) + "assert fresh", Output: "fresh input assertion passed"}}
	on := StrongVerifierPromptWithConfig(cfg, small, history, "")
	for _, want := range []string{"GENERALISATION CHECKLIST", "EVERY explicit property", "Visible examples", "types, shapes, file formats, field names, ordering", "FRESH_INPUTS:", "PROPERTY:", "assert fresh", "fresh input assertion passed", small.Request} {
		if !strings.Contains(on, want) {
			t.Errorf("missing %q", want)
		}
	}
	t.Setenv("EXCALIBUR_GENERALIZE_CHECKS", "off")
	cfg = DefaultSelfVerifyConfig()
	if cfg.EnableGeneralizeChecks {
		t.Fatal("off ignored")
	}
	off := StrongVerifierPromptWithConfig(cfg, small, history, "")
	if strings.Contains(off, "GENERALISATION CHECKLIST") || strings.Contains(off, "FRESH_INPUTS:") {
		t.Fatal("ablation still enabled")
	}
	t.Setenv("EXCALIBUR_GENERALIZE_CHECKS", "on")
	if !DefaultSelfVerifyConfig().EnableGeneralizeChecks {
		t.Fatal("on ignored")
	}
}

func TestGeneralizableInstructionBackstop(t *testing.T) {
	for _, request := range []string{"Implement a function that sorts integers.", "Create a solver for random instances", "Transform input arrays", "A CLI converts rows"} {
		if !HasGeneralizableInstruction(request) {
			t.Errorf("method missed: %s", request)
		}
	}
	for _, request := range []string{"say hi", "build the project with make", "write hello"} {
		if HasGeneralizableInstruction(request) {
			t.Errorf("non-method backstop: %s", request)
		}
	}
}
