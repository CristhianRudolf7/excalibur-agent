package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const DefaultConversation = "cnv_web_excalibur"

var (
	ErrNotFound   = errors.New("agent not found")
	ErrLastActive = errors.New("the last active agent cannot be archived")
	ErrArchived   = errors.New("agent is archived")
	ErrAmbiguous  = errors.New("agent name is ambiguous; use its id")
)

// PersonalAgent owns an independent chat and persona, sharing the user's memory.
// It is separate from the execution identities used by the orchestrator.
type PersonalAgent struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	Personality    string     `json:"personality"`
	Avatar         AvatarSpec `json:"avatar"`
	Favorite       bool       `json:"favorite"`
	Archived       bool       `json:"archived"`
	ConversationID string     `json:"conversation_id"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastMessageAt  *time.Time `json:"last_message_at"`
	Unread         int        `json:"unread"`
	LastPreview    string     `json:"last_preview"`
}

type CreateParams struct {
	Name        string      `json:"name"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Personality string      `json:"personality,omitempty"`
	Avatar      *AvatarSpec `json:"avatar,omitempty"`
}
type UpdateParams struct {
	ID          string      `json:"id"`
	Name        *string     `json:"name,omitempty"`
	Title       *string     `json:"title,omitempty"`
	Description *string     `json:"description,omitempty"`
	Personality *string     `json:"personality,omitempty"`
	Avatar      *AvatarSpec `json:"avatar,omitempty"`
}
type RegistryDB interface {
	store.Writer
	store.ReadPool
}

// Registry serializes state changes with the daemon's porter. A failed write
// leaves its in-memory view untouched. Publish runs only after commit.
type Registry struct {
	mu      sync.Mutex
	db      RegistryDB
	rows    map[string]PersonalAgent
	publish func(string, any)
}

func OpenRegistry(ctx context.Context, db RegistryDB, defaultName string, publish func(string, any)) (*Registry, error) {
	r := &Registry{db: db, rows: map[string]PersonalAgent{}, publish: publish}
	if db != nil {
		err := db.ReadInteractive(ctx, func(ctx context.Context, q store.Queryer) error {
			rows, err := q.Query(ctx, `SELECT data_json FROM personal_agents ORDER BY id`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var raw string
				if err := rows.Scan(&raw); err != nil {
					return err
				}
				var a PersonalAgent
				if err := json.Unmarshal([]byte(raw), &a); err != nil {
					return err
				}
				r.rows[a.ID] = a
			}
			return rows.Err()
		})
		if err != nil {
			return nil, err
		}
	}
	if len(r.rows) == 0 {
		if strings.TrimSpace(defaultName) == "" {
			defaultName = "Excalibur"
		}
		a, err := newPersonal(CreateParams{Name: defaultName})
		if err != nil {
			return nil, err
		}
		a.Favorite = true
		a.ConversationID = DefaultConversation
		// Existing messages keep their identifiers and conversation. Only metadata
		// is seeded; no history is moved, duplicated or marked unread at migration.
		if db != nil {
			err = db.ReadInteractive(ctx, func(ctx context.Context, q store.Queryer) error {
				rows, e := q.Query(ctx, `SELECT text,sent_at_ms FROM messages WHERE conversation_id=? ORDER BY sent_at_ms DESC,id DESC LIMIT 1`, DefaultConversation)
				if e != nil {
					return e
				}
				defer rows.Close()
				if rows.Next() {
					var text string
					var ms int64
					if e = rows.Scan(&text, &ms); e != nil {
						return e
					}
					a.LastPreview = preview(text)
					if ms > 0 {
						at := time.UnixMilli(ms).UTC()
						a.LastMessageAt = &at
					}
				}
				return rows.Err()
			})
			if err != nil {
				return nil, err
			}
		}
		if err := r.commit(ctx, map[string]PersonalAgent{a.ID: a}); err != nil {
			return nil, err
		}
	}
	return r, nil
}
func newPersonal(p CreateParams) (PersonalAgent, error) {
	now := time.Now().UTC()
	a := PersonalAgent{ID: string(types.NewAgentID()), Name: strings.TrimSpace(p.Name), Title: p.Title, Description: p.Description, Personality: p.Personality, Avatar: DefaultAvatar(), CreatedAt: now, UpdatedAt: now}
	a.ConversationID = "cnv_agent_" + a.ID
	if p.Avatar != nil {
		a.Avatar = *p.Avatar
	}
	return validatePersonal(a)
}
func validatePersonal(a PersonalAgent) (PersonalAgent, error) {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || len([]rune(a.Name)) > 120 {
		return a, fmt.Errorf("agent name must contain 1 to 120 characters")
	}
	if len([]rune(a.Title)) > 200 || len([]rune(a.Description)) > 4000 || len([]rune(a.Personality)) > 8000 {
		return a, fmt.Errorf("agent persona exceeds its size limit")
	}
	avatar, err := ValidateAvatar(a.Avatar)
	a.Avatar = avatar
	return a, err
}
func preview(text string) string {
	r := []rune(strings.Join(strings.Fields(text), " "))
	if len(r) > 240 {
		r = r[:240]
	}
	return string(r)
}
func cloneAgent(a PersonalAgent) PersonalAgent {
	if a.LastMessageAt != nil {
		at := *a.LastMessageAt
		a.LastMessageAt = &at
	}
	return a
}
func (r *Registry) copyRows() map[string]PersonalAgent {
	out := map[string]PersonalAgent{}
	for id, a := range r.rows {
		out[id] = cloneAgent(a)
	}
	return out
}
func (r *Registry) commit(ctx context.Context, next map[string]PersonalAgent) error {
	return r.commitWith(ctx, next, nil)
}
func (r *Registry) commitWith(ctx context.Context, next map[string]PersonalAgent, write store.WriteFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.db != nil {
		if err := r.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
			if write != nil {
				if err := write(ctx, ex); err != nil {
					return err
				}
			}
			for _, a := range next {
				raw, err := json.Marshal(a)
				if err != nil {
					return err
				}
				if _, err = ex.Exec(ctx, `INSERT INTO personal_agents(id,conversation_id,data_json) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data_json=excluded.data_json`, a.ID, a.ConversationID, string(raw)); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	r.rows = next
	return nil
}
func (r *Registry) emit(kind string, payload any) {
	if r.publish != nil {
		r.publish(kind, payload)
	}
}
func (r *Registry) List(includeArchived bool) []PersonalAgent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []PersonalAgent{}
	for _, a := range r.rows {
		if includeArchived || !a.Archived {
			out = append(out, cloneAgent(a))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}
func (r *Registry) Get(id string) (PersonalAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.rows[id]
	if !ok {
		return a, ErrNotFound
	}
	return cloneAgent(a), nil
}
func (r *Registry) ByConversation(conv string) (PersonalAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.rows {
		if a.ConversationID == conv {
			return cloneAgent(a), nil
		}
	}
	return PersonalAgent{}, ErrNotFound
}
func (r *Registry) Favorite() PersonalAgent {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range r.rows {
		if a.Favorite && !a.Archived {
			return cloneAgent(a)
		}
	}
	return PersonalAgent{}
}
func (r *Registry) Resolve(ref string) (PersonalAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a, ok := r.rows[ref]; ok {
		if a.Archived {
			return a, ErrArchived
		}
		return cloneAgent(a), nil
	}
	var found PersonalAgent
	for _, a := range r.rows {
		if !a.Archived && strings.EqualFold(a.Name, strings.TrimSpace(ref)) {
			if found.ID != "" {
				return PersonalAgent{}, ErrAmbiguous
			}
			found = a
		}
	}
	if found.ID == "" {
		return found, ErrNotFound
	}
	return cloneAgent(found), nil
}
func (r *Registry) Create(ctx context.Context, p CreateParams) (PersonalAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, err := newPersonal(p)
	if err != nil {
		return a, err
	}
	next := r.copyRows()
	next[a.ID] = a
	if err = r.commit(ctx, next); err != nil {
		return PersonalAgent{}, err
	}
	r.emit("agents.created", a)
	return a, nil
}
func (r *Registry) Update(ctx context.Context, p UpdateParams) (PersonalAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.rows[p.ID]
	if !ok {
		return a, ErrNotFound
	}
	if p.Name != nil {
		a.Name = *p.Name
	}
	if p.Title != nil {
		a.Title = *p.Title
	}
	if p.Description != nil {
		a.Description = *p.Description
	}
	if p.Personality != nil {
		a.Personality = *p.Personality
	}
	if p.Avatar != nil {
		a.Avatar = *p.Avatar
	}
	a, err := validatePersonal(a)
	if err != nil {
		return a, err
	}
	a.UpdatedAt = time.Now().UTC()
	next := r.copyRows()
	next[a.ID] = a
	if err = r.commit(ctx, next); err != nil {
		return PersonalAgent{}, err
	}
	r.emit("agents.updated", a)
	return cloneAgent(a), nil
}
func (r *Registry) Archive(ctx context.Context, id string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.rows[id]
	if !ok {
		return "", ErrNotFound
	}
	if a.Archived {
		for _, v := range r.rows {
			if v.Favorite {
				return v.ID, nil
			}
		}
	}
	var candidates []PersonalAgent
	favorite := ""
	for _, v := range r.rows {
		if v.ID != id && !v.Archived {
			candidates = append(candidates, v)
			if v.Favorite {
				favorite = v.ID
			}
		}
	}
	if len(candidates) == 0 {
		return "", ErrLastActive
	}
	wasFavorite := a.Favorite
	next := r.copyRows()
	a.Archived = true
	a.Favorite = false
	a.UpdatedAt = time.Now().UTC()
	next[id] = a
	if favorite == "" {
		sort.Slice(candidates, func(i, j int) bool {
			x, y := candidates[i], candidates[j]
			if x.LastMessageAt != nil || y.LastMessageAt != nil {
				if x.LastMessageAt == nil {
					return false
				}
				if y.LastMessageAt == nil {
					return true
				}
				if !x.LastMessageAt.Equal(*y.LastMessageAt) {
					return x.LastMessageAt.After(*y.LastMessageAt)
				}
			}
			if x.CreatedAt.Equal(y.CreatedAt) {
				return x.ID < y.ID
			}
			return x.CreatedAt.Before(y.CreatedAt)
		})
		chosen := candidates[0]
		chosen.Favorite = true
		chosen.UpdatedAt = a.UpdatedAt
		next[chosen.ID] = chosen
		favorite = chosen.ID
	}
	if err := r.commit(ctx, next); err != nil {
		return "", err
	}
	r.emit("agents.archived", a)
	if wasFavorite {
		r.emit("agents.favorite_changed", map[string]string{"favorite_id": favorite})
	}
	return favorite, nil
}
func (r *Registry) Restore(ctx context.Context, id string) (PersonalAgent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.rows[id]
	if !ok {
		return a, ErrNotFound
	}
	if !a.Archived {
		return cloneAgent(a), nil
	}
	a.Archived = false
	a.UpdatedAt = time.Now().UTC()
	next := r.copyRows()
	next[id] = a
	if err := r.commit(ctx, next); err != nil {
		return PersonalAgent{}, err
	}
	r.emit("agents.restored", a)
	return cloneAgent(a), nil
}
func (r *Registry) SetFavorite(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.rows[id]
	if !ok {
		return ErrNotFound
	}
	if a.Archived {
		return ErrArchived
	}
	if a.Favorite {
		return nil
	}
	next := r.copyRows()
	for key, a := range next {
		a.Favorite = key == id
		a.UpdatedAt = time.Now().UTC()
		next[key] = a
	}
	if err := r.commit(ctx, next); err != nil {
		return err
	}
	r.emit("agents.favorite_changed", map[string]string{"favorite_id": id})
	return nil
}

// ObserveMessage advances recency for all messages, incrementing unread only
// for assistant output and relays. Call once after a message is persisted.
func (r *Registry) ObserveMessage(ctx context.Context, conv, text string, at time.Time, unread bool) error {
	return r.PersistMessage(ctx, conv, text, at, unread, nil)
}

// PersistMessage commits message data and its unread metadata in one transaction.
func (r *Registry) PersistMessage(ctx context.Context, conv, text string, at time.Time, unread bool, write store.WriteFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.copyRows()
	for id, a := range next {
		if a.ConversationID != conv {
			continue
		}
		if a.LastMessageAt == nil || !at.Before(*a.LastMessageAt) {
			at = at.UTC()
			a.LastMessageAt = &at
			a.LastPreview = preview(text)
		}
		if unread {
			a.Unread++
		}
		next[id] = a
		if err := r.commitWith(ctx, next, write); err != nil {
			return err
		}
		r.emit("agents.unread", map[string]any{"agent_id": id, "unread": a.Unread, "last_preview": a.LastPreview, "last_message_at": a.LastMessageAt})
		return nil
	}
	return r.commitWith(ctx, next, write)
}
func (r *Registry) MarkRead(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.rows[id]
	if !ok {
		return ErrNotFound
	}
	a.Unread = 0
	next := r.copyRows()
	next[id] = a
	if err := r.commit(ctx, next); err != nil {
		return err
	}
	r.emit("agents.unread", map[string]any{"agent_id": id, "unread": 0, "last_preview": a.LastPreview, "last_message_at": a.LastMessageAt})
	return nil
}
