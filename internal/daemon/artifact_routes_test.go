package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func artifactRPC(t *testing.T, gate *gateway.Server, method string, params any) api.Response {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(string(raw)))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	gate.ServeHTTP(response, req)
	var out api.Response
	if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, response.Body.String())
	}
	return out
}

func TestArtifactsRPCContentAndOrigin(t *testing.T) {
	for _, format := range []string{"svg", "png", "pdf"} {
		t.Run(format, func(t *testing.T) {
			ctx := context.Background()
			store := journal.NewMemStore()
			collector := &artifactStore{journal: store}
			ref := artifactFixture(t, "architecture")
			ref.Format = format
			content := []byte("<svg>architecture</svg>")
			if format != "svg" {
				content = []byte{0, 255, 1, 2, 3}
			}
			if err := os.WriteFile(ref.Path, content, 0600); err != nil {
				t.Fatal(err)
			}
			added, err := collector.collect(ctx, "own-session", "run1", "render", []tools.ArtifactRef{ref})
			if err != nil {
				t.Fatal(err)
			}
			// A fresh backend over the same journal has no transient identity cache.
			backend := NewDaemonBackend(nil, nil, nil)
			backend.artifacts = &artifactStore{journal: store}
			gate := gateway.New(gateway.DefaultConfig(), backend, nil, nil)
			params := api.ArtifactsGetParams{ConversationID: "own-session", RunID: "run1", ID: added[0].ID}
			// Fetch must return the produced snapshot, even when the path disappears.
			if err := os.Remove(ref.Path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ref.SourcePath, []byte("replacement source"), 0600); err != nil {
				t.Fatal(err)
			}
			response := artifactRPC(t, gate, api.MethodArtifactsGet, params)
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			var got api.ArtifactsGetResult
			if err := json.Unmarshal(response.Result, &got); err != nil {
				t.Fatal(err)
			}
			wantContent := string(content)
			wantEncoding := "utf-8"
			if format != "svg" {
				wantContent = base64.StdEncoding.EncodeToString(content)
				wantEncoding = "base64"
			}
			if got.Content != wantContent || got.Encoding != wantEncoding || got.Source != "graph TD; A-->B" || got.Artifact.ID != params.ID {
				t.Fatalf("result=%+v", got)
			}
			for _, test := range []struct {
				name   string
				params api.ArtifactsGetParams
			}{
				{"unknown", api.ArtifactsGetParams{ConversationID: "own-session", RunID: "run1", ID: "art_00000000000000000000000000000000"}},
				{"other session", api.ArtifactsGetParams{ConversationID: "other-session", RunID: "run1", ID: params.ID}},
				{"other run", api.ArtifactsGetParams{ConversationID: "own-session", RunID: "run2", ID: params.ID}},
			} {
				t.Run(test.name, func(t *testing.T) {
					res := artifactRPC(t, gate, api.MethodArtifactsGet, test.params)
					if res.Error == nil || res.Error.Code != api.CodeUnknownTarget {
						t.Fatalf("response=%+v", res)
					}
				})
			}
			for _, bad := range []any{
				map[string]any{"conversation_id": "own-session", "run_id": "run1", "id": "../../etc/passwd"},
				map[string]any{"conversation_id": "../other-session", "run_id": "run1", "id": params.ID},
				map[string]any{"conversation_id": "own-session", "run_id": "../run1", "id": params.ID},
				map[string]any{"conversation_id": "own-session", "run_id": "run1", "id": params.ID, "path": "/etc/passwd"},
				map[string]any{"id": params.ID}, nil, []string{"invalid"},
			} {
				res := artifactRPC(t, gate, api.MethodArtifactsGet, bad)
				if res.Error == nil || res.Error.Code != api.CodeInvalidParams {
					t.Fatalf("invalid request accepted: %+v", res)
				}
			}
			res := artifactRPC(t, gate, api.MethodArtifactsList, api.ArtifactsListParams{ConversationID: "own-session"})
			if res.Error != nil {
				t.Fatal(res.Error)
			}
			var list api.ArtifactsListResult
			if err := json.Unmarshal(res.Result, &list); err != nil {
				t.Fatal(err)
			}
			if len(list.Artifacts) != 1 || list.Artifacts[0].ID != params.ID {
				t.Fatal(list)
			}
			res = artifactRPC(t, gate, api.MethodArtifactsList, api.ArtifactsListParams{ConversationID: "other-session", RunID: "run1"})
			if res.Error != nil {
				t.Fatal(res.Error)
			}
			if err := json.Unmarshal(res.Result, &list); err != nil {
				t.Fatal(err)
			}
			if list.Artifacts == nil || len(list.Artifacts) != 0 {
				t.Fatal(list)
			}
		})
	}
}

func TestArtifactsListOrderLimitAndFilter(t *testing.T) {
	ctx := context.Background()
	s := &artifactStore{journal: journal.NewMemStore()}
	var refs []tools.ArtifactRef
	for i := 0; i < maxTurnArtifacts; i++ {
		refs = append(refs, artifactFixture(t, "output"))
	}
	for _, run := range []string{"run1", "run2", "run3", "run4", "run5", "run6"} {
		if _, err := s.collect(ctx, "conv", run, "call", refs); err != nil {
			t.Fatal(err)
		}
	}
	b := NewDaemonBackend(nil, nil, nil)
	b.artifacts = s
	get := func(params api.ArtifactsListParams) api.ArtifactsListResult {
		t.Helper()
		raw, _ := json.Marshal(params)
		result, err := b.Passthrough(ctx, api.MethodArtifactsList, raw)
		if err != nil {
			t.Fatal(err)
		}
		return result.(api.ArtifactsListResult)
	}
	list := get(api.ArtifactsListParams{ConversationID: "conv"})
	if len(list.Artifacts) != 100 || list.Artifacts[0].RunID != "run6" || list.Artifacts[99].RunID != "run2" {
		t.Fatalf("list count=%d", len(list.Artifacts))
	}
	list = get(api.ArtifactsListParams{ConversationID: "conv", RunID: "run1"})
	if len(list.Artifacts) != 20 || list.Artifacts[0].URLOrPath != refs[0].Path || list.Artifacts[19].URLOrPath != refs[19].Path {
		t.Fatal(list)
	}
}

func TestArtifactsGetEnforcesStoredContentLimit(t *testing.T) {
	ctx := context.Background()
	for _, field := range []string{"content", "source"} {
		t.Run(field, func(t *testing.T) {
			s := &artifactStore{journal: journal.NewMemStore()}
			ref := artifactFixture(t, "oversized")
			rec, err := snapshotArtifact("conv", "run", "call", ref)
			if err != nil {
				t.Fatal(err)
			}
			if field == "content" {
				rec.Content = make([]byte, maxArtifactBytes+1)
			} else {
				rec.Source = strings.Repeat("x", maxArtifactSourceBytes+1)
			}
			if _, err := s.journal.Append(ctx, journal.Entry{RunID: "run", Type: artifactRecordType, Payload: journal.MarshalPayload(rec)}); err != nil {
				t.Fatal(err)
			}
			b := NewDaemonBackend(nil, nil, nil)
			b.artifacts = s
			gate := gateway.New(gateway.DefaultConfig(), b, nil, nil)
			response := artifactRPC(t, gate, api.MethodArtifactsGet, api.ArtifactsGetParams{ConversationID: "conv", RunID: "run", ID: rec.Artifact.ID})
			if response.Error == nil || response.Error.Code != api.CodeInternal || len(response.Result) != 0 {
				t.Fatalf("oversized response: %+v", response)
			}
		})
	}
}
