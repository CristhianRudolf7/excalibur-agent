package whiteboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func testService(t *testing.T) (*Service, *store.DB) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "board.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewService(NewStore(db), telemetry.NewBus(nil)), db
}
func memoryService() *Service      { return NewService(nil, telemetry.NewBus(nil)) }
func raw(s string) json.RawMessage { return json.RawMessage(s) }
func add(id, author string) Op {
	return Op{Op: "add", Elements: []json.RawMessage{raw(`{"id":"` + id + `","type":"rect","x":0,"y":0,"author":"` + author + `"}`)}}
}
func board(t *testing.T, s *Service) Board {
	t.Helper()
	b, err := s.Open(context.Background(), "conv-test", "", "", "panel")
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func code(t *testing.T, err error, want int) {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("error=%v want code %d", err, want)
	}
}
func TestStoreVersionsIdempotencyConflictAndRebase(t *testing.T) {
	s, db := testService(t)
	ctx := context.Background()
	b := board(t, s)
	b, err := s.Apply(ctx, b.ID, b.Version, "client-1", "user", []Op{add("user", "agent")}, true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if b.Version != 2 || b.Elements[0].Author != "user" {
		t.Fatalf("bad version/author: %+v", b)
	}
	n := s.Bus.Len()
	retry, err := s.Apply(ctx, b.ID, 1, "client-1", "user", []Op{add("user", "user")}, false, 0)
	if err != nil || retry.Version != 2 || s.Bus.Len() != n {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	_, err = s.Apply(ctx, b.ID, 1, "stale", "user", []Op{add("stale", "user")}, false, 0)
	code(t, err, api.CodeConflict)
	b, err = s.Apply(ctx, b.ID, -1, "agent-1", "agent", []Op{add("agent", "user")}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err = s.Apply(ctx, b.ID, b.Version, "rebased", "user", []Op{{Op: "update", Elements: []json.RawMessage{raw(`{"id":"user","x":42}`)}}}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Elements) != 2 || b.Elements[0].X != 42 || b.Elements[1].Author != "agent" {
		t.Fatalf("LWW merge lost data: %+v", b)
	}
	// A new service over the same DB sees the committed version and durable retry key.
	restarted := NewService(NewStore(db), nil)
	got, err := restarted.Store.Get(ctx, b.ID)
	if err != nil || got.Version != b.Version {
		t.Fatalf("restart: %+v %v", got, err)
	}
	retry, err = restarted.Apply(ctx, b.ID, 1, "client-1", "user", []Op{add("user", "user")}, false, 0)
	if err != nil || retry.Version != 2 {
		t.Fatalf("durable retry: %+v %v", retry, err)
	}
	err = db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var versions, ops int
		if e := q.QueryRow(ctx, `SELECT count(*) FROM whiteboard_versions WHERE board_id=?`, b.ID).Scan(&versions); e != nil {
			return e
		}
		if e := q.QueryRow(ctx, `SELECT count(*) FROM whiteboard_ops WHERE board_id=?`, b.ID).Scan(&ops); e != nil {
			return e
		}
		if versions != 4 || ops != 4 {
			t.Fatalf("versions=%d ops=%d", versions, ops)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := s.Store.Version(ctx, b.ID, 1)
	if err != nil || len(checkpoint.Elements) != 0 || checkpoint.Version != 1 {
		t.Fatalf("replay checkpoint: %+v %v", checkpoint, err)
	}

	for _, ev := range s.Bus.Replay(types.Cursor{}) {
		var p map[string]any
		_ = json.Unmarshal(ev.Payload, &p)
		if p["conversation_id"] != "conv-test" {
			t.Fatalf("missing conversation: %s", ev.Payload)
		}
	}
}
func TestAtomicValidationAndLimits(t *testing.T) {
	s := memoryService()
	b := board(t, s)
	ctx := context.Background()
	bad := []Op{{Op: "bogus"}, {Op: "update", Elements: []json.RawMessage{raw(`{"id":"missing","x":1}`)}}, add("bad", "user")}
	bad[2].Elements[0] = raw(`{"id":"bad","type":"unknown","x":0,"y":0}`)
	for _, op := range bad {
		_, err := s.Apply(ctx, b.ID, b.Version, newID(), "user", []Op{add("first", "user"), op}, false, 0)
		if err == nil {
			t.Fatal("invalid operation accepted")
		}
		got, _ := s.Store.Get(ctx, b.ID)
		if got.Version != b.Version || len(got.Elements) != 0 {
			t.Fatal("partial write")
		}
	}
	many := Op{Op: "add"}
	for i := 0; i < 501; i++ {
		many.Elements = append(many.Elements, raw(`{}`))
	}
	_, err := s.Apply(ctx, b.ID, b.Version, newID(), "user", []Op{many}, false, 0)
	code(t, err, api.CodeInvalidParams)
	huge := add("huge", "user")
	huge.Elements[0] = raw(`{"id":"huge","type":"text","text":"` + strings.Repeat("a", MaxBytes) + `"}`)
	_, err = s.Apply(ctx, b.ID, b.Version, newID(), "user", []Op{huge}, false, 0)
	code(t, err, api.CodeInvalidParams)
}
func TestAllToolActions(t *testing.T) {
	s, db := testService(t)
	tool := &Tool{Service: s, ConversationID: "conv-test"}
	ctx := context.Background()
	invoke := func(args string) map[string]any {
		t.Helper()
		r, err := tool.Invoke(ctx, tools.Invocation{Arguments: raw(args)})
		if err != nil || r.Status != tools.StatusOK {
			t.Fatalf("%s: %+v %v", args, r, err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(r.Output), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := invoke(`{"action":"open","title":"Lesson","layout":"fullscreen"}`)
	id := out["board_id"].(string)
	invoke(`{"action":"open","board_id":"` + id + `"}`)
	invoke(`{"action":"add","animate":true,"step_ms":20,"elements":[{"id":"a","type":"rect","x":10,"y":20,"width":100,"height":50,"author":"user"}]}`)
	invoke(`{"action":"update","elements":[{"id":"a","text":"A"}]}`)
	invoke(`{"action":"focus","ids":["a"],"zoom":2}`)
	invoke(`{"action":"highlight","ids":["a"],"style":"laser","duration_ms":20}`)
	invoke(`{"action":"annotate","target_id":"a","text":"Start here"}`)
	invoke(`{"action":"step","delay_ms":1,"steps":[{"op":"add","elements":[{"id":"b","type":"sticky","x":0,"y":200}]},{"op":"update","elements":[{"id":"b","text":"B"}]},{"op":"focus","ids":["b"]},{"op":"highlight","ids":["b"],"style":"box"},{"op":"remove","ids":["b"]}]}`)
	invoke(`{"action":"rename","title":"Renamed"}`)
	read := invoke(`{"action":"read"}`)
	if len(read["elements"].([]any)) != 3 {
		t.Fatal(read)
	}
	invoke(`{"action":"list"}`)
	for _, format := range []string{"json", "svg"} {
		out = invoke(`{"action":"export","format":"` + format + `"}`)
		r, err := db.Blob().Open(ctx, out["blob_ref"].(string))
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
	}
	invoke(`{"action":"remove","ids":["a"]}`)
	invoke(`{"action":"clear"}`)
	invoke(`{"action":"set_scene","elements":[{"id":"last","type":"math","x":0,"y":0,"latex":"x^2"}]}`)
	invoke(`{"action":"close"}`)
	invoke(`{"action":"delete"}`)
	_, err := s.Store.Get(ctx, id)
	code(t, err, api.CodeUnknownTarget)
	seen := map[types.EventType]bool{}
	for _, ev := range s.Bus.Replay(types.Cursor{}) {
		seen[ev.Type] = true
		var p map[string]any
		_ = json.Unmarshal(ev.Payload, &p)
		if p["conversation_id"] != "conv-test" {
			t.Fatal(string(ev.Payload))
		}
		if ev.Type == "whiteboard.ops" && p["animate"] == true && p["step_ms"] != float64(20) {
			t.Fatal(p)
		}
	}
	for _, name := range []types.EventType{"whiteboard.opened", "whiteboard.ops", "whiteboard.focus", "whiteboard.highlight", "whiteboard.closed"} {
		if !seen[name] {
			t.Fatalf("missing %s", name)
		}
	}
}
func TestReadSnapshotRoundTripTimeoutCancellation(t *testing.T) {
	s, db := testService(t)
	b := board(t, s)
	ctx := context.Background()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	ref, err := db.Blob().Put(ctx, &buf)
	if err != nil {
		t.Fatal(err)
	}
	events := s.Bus.Subscribe(16)
	done := make(chan error, 1)
	safego.Go("whiteboard-test-snapshot", nil, func() {
		select {
		case ev := <-events:
			var p map[string]any
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				done <- err
				return
			}
			_, err := s.Op(ctx, api.MethodWhiteboardSnapshot, raw(`{"board_id":"`+b.ID+`","image_blob":"`+ref+`","request_id":"`+p["request_id"].(string)+`"}`))
			done <- err
		case <-time.After(time.Second):
			done <- errors.New("missing snapshot event")
		}
	})
	out, err := s.Read(ctx, b, true)
	if err != nil || out["image_blob"] != ref {
		t.Fatalf("snapshot=%+v %v", out, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s.SnapshotTimeout = time.Millisecond
	out, err = s.Read(ctx, b, true)
	if err != nil || out["image_timeout"] != true || out["elements"] == nil {
		t.Fatalf("timeout=%+v %v", out, err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.Read(cancelCtx, b, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s.mu.Lock()
	n := len(s.pending)
	s.mu.Unlock()
	if n != 0 {
		t.Fatal("leaked pending snapshot")
	}
}
func TestKeepUserAndInputErrors(t *testing.T) {
	s := memoryService()
	ctx := context.Background()
	b := board(t, s)
	b, err := s.Apply(ctx, b.ID, -1, newID(), "user", []Op{add("stroke", "agent")}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err = s.Apply(ctx, b.ID, -1, newID(), "agent", []Op{add("agent", "user"), {Op: "clear", KeepUser: true}}, false, 0)
	if err != nil || len(b.Elements) != 1 || b.Elements[0].ID != "stroke" {
		t.Fatalf("clear: %+v %v", b, err)
	}
	tool := &Tool{Service: s, ConversationID: "conv-test"}
	for _, args := range []string{`{`, `{"action":"add","unknown":true}`, `{"action":"highlight","style":"bad"}`, `{"action":"step","steps":[{"op":"delete"}]}`, `{"action":"focus","zoom":-1}`, `{"action":"annotate","target_id":"missing","text":"bad"}`, `{"action":"export","format":"bad"}`, `{"action":"bogus"}`} {
		r, err := tool.Invoke(ctx, tools.Invocation{Arguments: raw(args)})
		if err != nil || r.Status != tools.StatusInputError {
			t.Fatalf("accepted %s: %+v %v", args, r, err)
		}
	}
	t.Setenv("EXCALIBUR_WHITEBOARD", "off")
	r, _ := tool.Invoke(ctx, tools.Invocation{Arguments: raw(`{"action":"list"}`)})
	if r.Status != tools.StatusDenied {
		t.Fatal(r)
	}
}
func TestAPIMethodValidation(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	b := board(t, s)
	for _, method := range []string{api.MethodWhiteboardGet, api.MethodWhiteboardApply, api.MethodWhiteboardSnapshot, api.MethodWhiteboardClose} {
		_, err := s.Op(ctx, method, raw(`{}`))
		code(t, err, api.CodeInvalidParams)
	}
	_, err := s.Op(ctx, api.MethodWhiteboardApply, raw(`{"board_id":"`+b.ID+`","client_op_id":"x","ops":[{"op":"clear"}]}`))
	code(t, err, api.CodeInvalidParams)
	for _, method := range []string{api.MethodWhiteboardList, api.MethodWhiteboardGet} {
		_, err := s.Op(ctx, method, raw(`{"board_id":"`+b.ID+`"}`))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.Op(ctx, api.MethodWhiteboardApply, raw(`{"board_id":"`+b.ID+`","base_version":1,"client_op_id":"api-op","ops":[{"op":"add","elements":[{"id":"u","type":"freedraw","x":1,"y":2,"points":[[0,0],[2,3]]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ = s.Store.Get(ctx, b.ID)
	out, err := s.Read(ctx, b, false)
	if err != nil || len(out["user_strokes"].([]Element)) != 1 {
		t.Fatal(out, err)
	}
}

func TestAllPrimitiveTypesAndSVG(t *testing.T) {
	s := memoryService()
	b := board(t, s)
	ctx := context.Background()
	op := Op{Op: "add"}
	for _, kind := range []string{"rect", "ellipse", "diamond", "line", "arrow", "text", "freedraw", "image", "frame", "math", "mermaid", "code", "sticky"} {
		e := Element{ID: kind, Type: kind, X: 10, Y: 20, Width: 100, Height: 50, Text: "<script>& test", Points: [][2]float64{{0, 0}, {20, 30}}, Author: "agent"}
		data, _ := json.Marshal(e)
		op.Elements = append(op.Elements, data)
	}
	b, err := s.Apply(ctx, b.ID, b.Version, newID(), "agent", []Op{op}, false, 0)
	if err != nil || len(b.Elements) != 13 {
		t.Fatalf("primitives: %+v %v", b, err)
	}
	svg := renderSVG(b)
	if strings.Contains(svg, "<script>") || !strings.Contains(svg, "&lt;script&gt;") {
		t.Fatalf("unescaped export: %s", svg)
	}
	b, err = s.Present(ctx, b.ID, "focus", map[string]any{"ids": []string{"rect"}, "zoom": float64(2)})
	if err != nil || b.Viewport == nil || b.Viewport.Zoom != 2 || b.Viewport.X != 60 {
		t.Fatalf("viewport: %+v %v", b, err)
	}
}
func TestRestartFromDiskAndMigrationUpgrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.db")
	old := migrations.New()
	for _, m := range migrations.Main().Ordered() {
		if m.Version < 8 {
			if err := old.Register(m); err != nil {
				t.Fatal(err)
			}
		}
	}
	db, err := store.Open(ctx, path, store.DefaultOptions(), old)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(NewStore(db), nil)
	b := board(t, s)
	b, err = s.Apply(ctx, b.ID, b.Version, "durable", "user", []Op{add("u", "user")}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s = NewService(NewStore(db), nil)
	got, err := s.Store.Get(ctx, b.ID)
	if err != nil || got.Version != 2 || len(got.Elements) != 1 {
		t.Fatalf("disk restart: %+v %v", got, err)
	}
	got, err = s.Apply(ctx, b.ID, 1, "durable", "user", []Op{{Op: "clear"}}, false, 0)
	if err != nil || got.Version != 2 {
		t.Fatalf("disk idempotency: %+v %v", got, err)
	}
}
func TestConcurrentWritersConflictAtomically(t *testing.T) {
	s, db := testService(t)
	other := NewService(NewStore(db), nil)
	b := board(t, s)
	ctx := context.Background()
	results := make(chan error, 2)
	for i, svc := range []*Service{s, other} {
		index := i
		service := svc
		safego.Go("whiteboard-concurrent-test", nil, func() {
			_, err := service.Apply(ctx, b.ID, 1, []string{"one", "two"}[index], "user", []Op{add([]string{"a", "b"}[index], "user")}, false, 0)
			results <- err
		})
	}
	success, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else {
			code(t, err, api.CodeConflict)
			conflicts++
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
}
func TestStepInterruptionPreservesCompletedOperations(t *testing.T) {
	s := memoryService()
	b := board(t, s)
	tool := &Tool{Service: s, ConversationID: "conv-test"}
	events := s.Bus.Subscribe(16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan tools.Result, 1)
	safego.Go("whiteboard-step-test", nil, func() {
		r, _ := tool.Invoke(ctx, tools.Invocation{Arguments: raw(`{"action":"step","delay_ms":10000,"steps":[{"op":"add","elements":[{"id":"first","type":"rect","x":0,"y":0}]},{"op":"remove","ids":["first"]}]}`)})
		done <- r
	})
	select {
	case <-events:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("first step did not execute")
	}
	select {
	case r := <-done:
		if r.Status != tools.StatusCanceled {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		t.Fatal("step ignored cancellation")
	}
	got, err := s.Store.Get(context.Background(), b.ID)
	if err != nil || len(got.Elements) != 1 || got.Elements[0].ID != "first" {
		t.Fatalf("partial sequence lost: %+v %v", got, err)
	}
}

func TestPNGExportAndSnapshotCorrelation(t *testing.T) {
	s, db := testService(t)
	b := board(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	ref, err := db.Blob().Put(ctx, &buf)
	if err != nil {
		t.Fatal(err)
	}
	events := s.Bus.Subscribe(8)
	reply := make(chan error, 1)
	safego.Go("whiteboard-png-export", nil, func() {
		select {
		case ev := <-events:
			var p api.WhiteboardSnapshotRequestedEvent
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				reply <- err
				return
			}
			if err := s.Snapshot(ctx, b.ID, "wrong-request", ref); err == nil {
				reply <- errors.New("accepted wrong correlation")
				return
			}
			reply <- s.Snapshot(ctx, b.ID, p.RequestID, ref)
		case <-ctx.Done():
			reply <- ctx.Err()
		}
	})
	tool := &Tool{Service: s, ConversationID: "conv-test"}
	r, err := tool.Invoke(ctx, tools.Invocation{Arguments: raw(`{"action":"export","format":"png"}`)})
	if err != nil || r.Status != tools.StatusOK {
		t.Fatalf("PNG export: %+v %v", r, err)
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(r.Output), &out)
	if out["blob_ref"] != ref || out["mime"] != "image/png" {
		t.Fatal(out)
	}
	if err := <-reply; err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(ctx, b.ID, "expired", ref); err == nil {
		t.Fatal("accepted expired snapshot")
	}
}
func TestActiveBoardsAndConversationIsolation(t *testing.T) {
	s := memoryService()
	ctx := context.Background()
	first := board(t, s)
	second, err := s.Open(ctx, "conv-test", "", "Second", "panel")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("title did not create another board")
	}
	tool := &Tool{Service: s, ConversationID: "conv-test"}
	r, _ := tool.Invoke(ctx, tools.Invocation{Arguments: raw(`{"action":"add","elements":[{"id":"active","type":"rect","x":0,"y":0}]}`)})
	if r.Status != tools.StatusOK {
		t.Fatal(r)
	}
	got, err := s.Store.Get(ctx, second.ID)
	if err != nil || len(got.Elements) != 1 {
		t.Fatalf("implicit operation used wrong board: %+v %v", got, err)
	}
	_, err = s.Open(ctx, "other", first.ID, "", "panel")
	code(t, err, api.CodeInvalidParams)
	r, _ = tool.Invoke(WithConversation(ctx, "other"), tools.Invocation{Arguments: raw(`{"action":"get","board_id":"` + first.ID + `"}`)})
	if r.Status != tools.StatusInputError {
		t.Fatal("cross-conversation access accepted")
	}
	boards, err := s.Store.List(ctx, "other")
	if err != nil || len(boards) != 0 {
		t.Fatal(boards, err)
	}
}
