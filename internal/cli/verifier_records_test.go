package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type errorUsageEndpoint struct{ usage providers.Usage }

func (e errorUsageEndpoint) Chat(context.Context, types.ModelRole, providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	return providers.ChatResponse{Usage: e.usage}, models.CallMeta{}, &providers.ProviderError{Kind: providers.KindTruncated}
}

func TestVerifierErrorRecordsPreserveAvailableUsageAndSite(t *testing.T) {
	for _, usage := range []providers.Usage{{}, {Input: 123, Output: 8192, Reasoning: 8192}} {
		dir := t.TempDir()
		rec, err := eval.NewTrialRecorder(dir)
		if err != nil {
			t.Fatal(err)
		}
		m := recordingModelEndpoint{base: errorUsageEndpoint{usage: usage}, run: &ExecRun{Recorder: rec}}
		_, _, err = m.Chat(engine.WithModelCallSite(context.Background(), "verifier.strong.retry"), types.RoleVerifier, providers.ChatRequest{})
		var pe *providers.ProviderError
		if !errors.As(err, &pe) {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "model_calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		var call eval.ModelCall
		if err = json.Unmarshal(raw, &call); err != nil {
			t.Fatal(err)
		}
		if call.CallSite != "verifier.strong.retry" || call.FinishReason != "length" || call.ProviderError != "truncated" || call.InputTokens != usage.Input || call.OutputTokens != usage.Output || call.ReasoningTok != usage.Reasoning || call.UsageAvailable != (usage != (providers.Usage{})) {
			t.Fatalf("record=%+v usage=%+v", call, usage)
		}
	}
}
