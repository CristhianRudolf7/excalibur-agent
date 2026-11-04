package whiteboard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type conversationKey struct{}

func WithConversation(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, conversationKey{}, id)
}
func Conversation(ctx context.Context) string {
	id, _ := ctx.Value(conversationKey{}).(string)
	return id
}
func Enabled() bool {
	v := strings.ToLower(os.Getenv("EXCALIBUR_WHITEBOARD"))
	return v != "0" && v != "false" && v != "off"
}
func newID() string            { var b [16]byte; _, _ = rand.Read(b[:]); return "wb_" + hex.EncodeToString(b[:]) }
func invalid(msg string) error { return &api.Error{Code: api.CodeInvalidParams, Message: msg} }

type pendingSnapshot struct {
	board string
	ch    chan string
}
type Service struct {
	Store           *Store
	Bus             *telemetry.Bus
	SnapshotTimeout time.Duration
	opMu            sync.Mutex
	mu              sync.Mutex
	pending         map[string]pendingSnapshot
	active          map[string]string
}

func NewService(s *Store, bus *telemetry.Bus) *Service {
	if s == nil {
		s = NewStore(nil)
	}
	return &Service{Store: s, Bus: bus, SnapshotTimeout: 8 * time.Second, pending: map[string]pendingSnapshot{}, active: map[string]string{}}
}
func (s *Service) emit(b Board, name string, fields map[string]any) error {
	if s.Bus == nil {
		return nil
	}
	if fields == nil {
		fields = map[string]any{}
	}
	fields["board_id"] = b.ID
	fields["conversation_id"] = b.ConversationID
	_, err := s.Bus.Publish(types.EventType("whiteboard."+name), nil, nil, fields)
	return err
}
func (s *Service) Open(ctx context.Context, conv, id, title, layout string) (Board, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if conv == "" {
		return Board{}, invalid("conversation context required")
	}
	if layout == "" {
		layout = "panel"
	}
	if layout != "panel" && layout != "fullscreen" {
		return Board{}, invalid("invalid layout")
	}
	if len(title) > 1000 {
		return Board{}, invalid("title exceeds 1000 bytes")
	}
	if id == "" && title == "" {
		boards, err := s.Store.List(ctx, conv)
		if err != nil {
			return Board{}, err
		}
		if len(boards) > 0 {
			id, err = s.Current(ctx, conv)
			if err != nil {
				return Board{}, err
			}
		}
	}
	var b Board
	var err error
	if id != "" {
		b, err = s.Store.Get(ctx, id)
		if err == nil && b.ConversationID != conv {
			err = invalid("board belongs to another conversation")
		}
		if err == nil {
			b, _, err = s.Store.mutate(ctx, id, newID(), "agent", map[string]any{"op": "open", "layout": layout}, func(b *Board) error {
				if b.ID == "" {
					return missing()
				}
				b.Version++
				b.UpdatedAt = time.Now().UTC()
				return nil
			})
		}
	} else {
		id = newID()
		if title == "" {
			title = "Whiteboard"
		}
		b, _, err = s.Store.mutate(ctx, id, newID(), "agent", map[string]any{"op": "open", "title": title, "layout": layout}, func(b *Board) error {
			*b = Board{ID: id, ConversationID: conv, Title: title, Version: 1, Elements: []Element{}, UpdatedAt: time.Now().UTC()}
			return nil
		})
	}
	if err != nil {
		return b, err
	}
	s.mu.Lock()
	s.active[conv] = b.ID
	s.mu.Unlock()
	return b, s.emit(b, "opened", map[string]any{"board": b, "layout": layout})
}
func (s *Service) Apply(ctx context.Context, id string, base int64, key, author string, ops []Op, animate bool, stepMS int) (Board, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if author != "agent" && author != "user" {
		return Board{}, invalid("invalid author")
	}
	if animate && stepMS == 0 {
		stepMS = 300
	}
	if stepMS < 0 || stepMS > 10000 {
		return Board{}, invalid("step_ms must be 0 to 10000")
	}
	if key == "" || len(key) > 200 {
		return Board{}, invalid("client_op_id required (max 200 bytes)")
	}
	raw, err := json.Marshal(ops)
	if err != nil || len(raw) > MaxBytes {
		return Board{}, invalid("operation exceeds 2 MiB")
	}
	// Work on an owned copy; cancellation can return while the porter finishes
	// its command, and normalization must not mutate the caller's slice.
	var owned []Op
	if err := json.Unmarshal(raw, &owned); err != nil {
		return Board{}, invalid(err.Error())
	}
	ops = owned
	b, dup, err := s.Store.mutate(ctx, id, key, author, &ops, func(b *Board) error {
		if b.ID == "" {
			return missing()
		}
		if base >= 0 && base != b.Version {
			return &api.Error{Code: api.CodeConflict, Message: "stale whiteboard version", Data: json.RawMessage(fmt.Sprintf(`{"current_version":%d}`, b.Version))}
		}
		normalized, err := applyOps(b, ops, author)
		if err != nil {
			return invalid(err.Error())
		}
		ops = normalized
		b.Version++
		b.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil || dup {
		return b, err
	}
	return b, s.emit(b, "ops", map[string]any{"version": b.Version, "ops": ops, "animate": animate, "step_ms": stepMS, "author": author})
}
func (s *Service) Rename(ctx context.Context, id, title string) (Board, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if strings.TrimSpace(title) == "" || len(title) > 1000 {
		return Board{}, invalid("title required (max 1000 bytes)")
	}
	b, _, err := s.Store.mutate(ctx, id, newID(), "agent", map[string]any{"op": "rename", "title": title}, func(b *Board) error {
		if b.ID == "" {
			return missing()
		}
		b.Title = title
		b.Version++
		b.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		return b, err
	}
	return b, s.emit(b, "opened", map[string]any{"board": b, "layout": "panel"})
}
func (s *Service) Close(ctx context.Context, id string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	b, _, err := s.Store.mutate(ctx, id, newID(), "agent", []map[string]string{{"op": "close"}}, func(b *Board) error {
		if b.ID == "" {
			return missing()
		}
		b.Version++
		b.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		return err
	}
	return s.emit(b, "closed", map[string]any{"version": b.Version})
}
func (s *Service) Delete(ctx context.Context, id string) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	b, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err = s.Store.Delete(ctx, id); err != nil {
		return err
	}
	return s.emit(b, "closed", map[string]any{"deleted": true, "version": b.Version + 1})
}
func (s *Service) Read(ctx context.Context, b Board, include bool) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := map[string]any{"board_id": b.ID, "version": b.Version, "elements": b.Elements}
	strokes := []Element{}
	for _, e := range b.Elements {
		if e.Author == "user" && e.Type == "freedraw" {
			strokes = append(strokes, e)
		}
	}
	out["user_strokes"] = strokes
	if !include {
		return out, nil
	}
	id := newID()
	p := pendingSnapshot{b.ID, make(chan string, 1)}
	s.mu.Lock()
	s.pending[id] = p
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	if err := s.emit(b, "snapshot_requested", map[string]any{"request_id": id, "version": b.Version}); err != nil {
		return nil, err
	}
	timeout := s.SnapshotTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ref := <-p.ch:
		out["image_blob"] = ref
	case <-timer.C:
		out["image_timeout"] = true
		out["notice"] = "Snapshot timed out; elements are available."
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return out, nil
}
func (s *Service) Snapshot(ctx context.Context, id, request, ref string) error {
	if s.Store.DB == nil {
		return invalid("snapshot blob storage unavailable")
	}
	info, err := s.Store.DB.Blob().Stat(ctx, ref)
	if err != nil || info.Size > 16<<20 {
		return invalid("snapshot blob missing or exceeds 16 MiB")
	}
	r, err := s.Store.DB.Blob().Open(ctx, ref)
	if err != nil {
		return invalid("unknown image_blob")
	}
	defer r.Close()
	cfg, err := png.DecodeConfig(io.LimitReader(r, MaxBytes))
	if err != nil || cfg.Width > 8192 || cfg.Height > 8192 {
		return invalid("snapshot must be a PNG up to 8192 pixels per dimension")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if request == "" {
		for k, p := range s.pending {
			if p.board == id {
				if request != "" {
					return invalid("request_id required for concurrent requests")
				}
				request = k
			}
		}
	}
	p, ok := s.pending[request]
	if !ok || p.board != id {
		return invalid("no matching snapshot request")
	}
	select {
	case p.ch <- ref:
	default:
	}
	return nil
}

// Present persists presentation checkpoints and viewport changes in the same
// version history as drawing operations, then notifies the UI.
func (s *Service) Present(ctx context.Context, id, name string, fields map[string]any) (Board, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	b, _, err := s.Store.mutate(ctx, id, newID(), "agent", map[string]any{"op": name, "presentation": fields}, func(b *Board) error {
		if b.ID == "" {
			return missing()
		}
		if name == "focus" {
			ids, _ := fields["ids"].([]string)
			selected := map[string]bool{}
			for _, id := range ids {
				selected[id] = true
			}
			var x, y float64
			rectRaw, _ := json.Marshal(fields["rect"])
			if string(rectRaw) != "null" {
				var r struct{ X, Y, Width, Height float64 }
				if err := json.Unmarshal(rectRaw, &r); err != nil {
					return err
				}
				if r.Width < 0 || r.Height < 0 {
					return invalid("focus rectangle dimensions must be positive")
				}
				x = r.X + r.Width/2
				y = r.Y + r.Height/2
			} else {
				first := true
				var maxX, maxY float64
				for _, e := range b.Elements {
					if len(selected) > 0 && !selected[e.ID] {
						continue
					}
					if first {
						x = e.X
						y = e.Y
						maxX = e.X + e.Width
						maxY = e.Y + e.Height
						first = false
					} else {
						if e.X < x {
							x = e.X
						}
						if e.Y < y {
							y = e.Y
						}
						if e.X+e.Width > maxX {
							maxX = e.X + e.Width
						}
						if e.Y+e.Height > maxY {
							maxY = e.Y + e.Height
						}
					}
				}
				x = (x + maxX) / 2
				y = (y + maxY) / 2
			}
			zoom, _ := fields["zoom"].(float64)
			if zoom == 0 {
				zoom = 1
				if b.Viewport != nil {
					zoom = b.Viewport.Zoom
				}
			}
			b.Viewport = &Viewport{X: x, Y: y, Zoom: zoom}
		}
		b.Version++
		b.UpdatedAt = time.Now().UTC()
		return nil
	})
	if err != nil {
		return b, err
	}
	fields["version"] = b.Version
	return b, s.emit(b, name, fields)
}

// Current resolves the explicitly opened board, or the persistent default.
func (s *Service) Current(ctx context.Context, conv string) (string, error) {
	s.mu.Lock()
	id := s.active[conv]
	s.mu.Unlock()
	if id != "" {
		if _, err := s.Store.Get(ctx, id); err == nil {
			return id, nil
		}
	}
	boards, err := s.Store.List(ctx, conv)
	if err != nil {
		return "", err
	}
	if len(boards) == 0 {
		return "", nil
	}
	return boards[0].ID, nil
}
