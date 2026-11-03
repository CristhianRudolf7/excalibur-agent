package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/coder/websocket"
)

func collectionRoutesTest(t *testing.T) (*CollectionRoutes, *telemetry.Bus) {
	t.Helper()
	bus := telemetry.NewBus(nil)
	s, err := NewCollectionRoutes(t.TempDir(), gateway.NewMemoryBlobs(), bus)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, bus
}
func TestCollectionRPCThroughGateway(t *testing.T) {
	s, bus := collectionRoutesTest(t)
	backend := NewDaemonBackend(nil, nil, bus)
	backend.AttachServices(BackendServices{Collections: s})
	server := httptest.NewServer(gateway.New(gateway.DefaultConfig(), backend, s.Blobs, bus))
	defer server.Close()
	call := func(method string, params any, target any) *api.Error {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "test", "method": method, "params": params})
		resp, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result api.Response
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Error == nil && target != nil {
			if err = json.Unmarshal(result.Result, target); err != nil {
				t.Fatal(err)
			}
		}
		return result.Error
	}
	must := func(method string, p any, out any) {
		t.Helper()
		if err := call(method, p, out); err != nil {
			t.Fatal(method, err)
		}
	}
	var created api.CollectionResult
	must(api.MethodCollectionsCreate, api.CollectionsCreateParams{Name: "Income", Fields: []api.CollectionField{{ID: "name", Name: "Name", Type: "text", Required: true}, {ID: "amount", Name: "Amount", Type: "money", Currency: "PEN"}}}, &created)
	id := created.Collection.ID
	var listed api.CollectionsListResult
	must(api.MethodCollectionsList, struct{}{}, &listed)
	if len(listed.Collections) != 1 {
		t.Fatal(listed)
	}
	var got api.CollectionResult
	must(api.MethodCollectionsGet, api.CollectionsIDParams{ID: id}, &got)
	if got.Collection.ID != id {
		t.Fatal(got)
	}
	name := "Payments"
	must(api.MethodCollectionsUpdate, api.CollectionsUpdateParams{ID: id, Name: &name}, &got)
	var fields api.CollectionsFieldsResult
	must(api.MethodCollectionsFields, api.CollectionsFieldsParams{ID: id, Add: []api.CollectionField{{ID: "note", Name: "Note", Type: "text"}}}, &fields)
	apiErr := call(api.MethodRecordsCreate, api.RecordsCreateParams{Collection: id, Values: map[string]any{"amount": "bad"}}, nil)
	if apiErr == nil || apiErr.Code != api.CodeInvalidParams {
		t.Fatal(apiErr)
	}
	data := apiErr.Data.(map[string]any)
	if len(data["errors"].([]any)) != 2 {
		t.Fatal(data)
	}
	var record api.RecordResult
	must(api.MethodRecordsCreate, api.RecordsCreateParams{Collection: id, Values: map[string]any{"name": "Salary", "amount": "500"}}, &record)
	rid := record.Record.ID
	must(api.MethodRecordsUpdate, api.RecordsUpdateParams{Collection: id, ID: rid, Values: map[string]any{"amount": "600"}}, &record)
	var detail api.RecordsGetResult
	must(api.MethodRecordsGet, api.RecordsIDParams{Collection: id, ID: rid}, &detail)
	if detail.Record.Values["amount"] != float64(600) || detail.Related == nil {
		t.Fatal(detail)
	}
	var rows api.RecordsListResult
	must(api.MethodRecordsList, api.RecordsListParams{Collection: id}, &rows)
	if rows.Total != 1 {
		t.Fatal(rows)
	}
	var summary api.CollectionsSummaryResult
	must(api.MethodCollectionsSummary, api.CollectionsIDParams{ID: id}, &summary)
	if summary.Cards == nil {
		t.Fatal("nil cards")
	}
	var exported api.CollectionsExportResult
	must(api.MethodCollectionsExport, api.CollectionsExportParams{ID: id, Format: "xlsx"}, &exported)
	var imported api.CollectionsImportResult
	must(api.MethodCollectionsImport, api.CollectionsImportParams{Collection: id, BlobDigest: exported.BlobDigest, DryRun: true}, &imported)
	if imported.Report.Skipped != 1 || imported.Report.Conflicts[0].Problem != "duplicate" {
		t.Fatal(imported)
	}
	must(api.MethodRecordsDelete, api.RecordsIDParams{Collection: id, ID: rid}, nil)
	must(api.MethodCollectionsDelete, api.CollectionsIDParams{ID: id}, nil)
	if err := call(api.MethodCollectionsGet, api.CollectionsIDParams{ID: id}, nil); err == nil || err.Code != api.CodeUnknownTarget {
		t.Fatal(err)
	}
	events := bus.Replay(types.Cursor{})
	if len(events) != 7 {
		t.Fatal("mutation event count", len(events), events)
	}
	for _, ev := range events {
		if ev.Type != api.EventCollectionsChanged {
			t.Fatal(ev)
		}
	}
}
func TestCollectionRPCInvalidParamsAndUnavailable(t *testing.T) {
	s, _ := collectionRoutesTest(t)
	for _, raw := range []string{`{`, `null`, `[]`, `{} {}`, `{"unknown":true}`} {
		if _, err := s.Op(context.Background(), api.MethodCollectionsList, json.RawMessage(raw)); err == nil || err.(*api.Error).Code != api.CodeInvalidParams {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	if _, err := (*CollectionRoutes)(nil).Op(context.Background(), api.MethodCollectionsList, nil); err == nil {
		t.Fatal("nil routes")
	}
	if _, err := s.Op(context.Background(), "collections.unknown", nil); err == nil || err.(*api.Error).Code != api.CodeMethodNotFound {
		t.Fatal(err)
	}
}
func TestCollectionChangedWebSocket(t *testing.T) {
	s, bus := collectionRoutesTest(t)
	backend := NewDaemonBackend(nil, nil, bus)
	backend.AttachServices(BackendServices{Collections: s})
	server := httptest.NewServer(gateway.New(gateway.DefaultConfig(), backend, s.Blobs, bus))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if err = conn.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":1,"method":"conversation.subscribe","params":{"types":["collections.changed"]}}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := s.Engine.Create(ctx, api.CollectionsCreateParams{Name: "Grades", Fields: []api.CollectionField{{ID: "name", Name: "Name", Type: "text"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(api.EventCollectionsChanged)) || !bytes.Contains(raw, []byte(c.ID)) {
		t.Fatalf("event: %s", raw)
	}
}
