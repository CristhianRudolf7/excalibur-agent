package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// Personal-assistant deferred tools (Sections 13.3 and 14.1, 14.9,
// 14.10). Every tool below runs against its capability contract and
// ships with a contract fake; the daemon swaps the fakes for real
// adapters without changing the tools. The eight deferred names match
// the Section 13.3 catalog (email, calendar, contact_message, meeting,
// github, userdb, gpu_training, nodes) and register through the same
// Registry as built-ins: there is no privileged path (Section 14.12).

// PersonalDeps bundles the capability backends of the personal tools.
// Nil entries fall back to deterministic memory fakes.
type PersonalDeps struct {
	Messaging capabilities.Capability
	Workspace capabilities.Capability
	Meetings  capabilities.Capability
	GitHub    capabilities.Capability
	Documents capabilities.Capability
	GPU       capabilities.Capability
	Nodes     *capabilities.NodeRegistry
}

// PersonalFakes returns deps wired to every memory fake.
func PersonalFakes() *PersonalDeps {
	return &PersonalDeps{
		Messaging: capabilities.NewMessagingFake(),
		Workspace: capabilities.NewWorkspaceFake(),
		Meetings:  capabilities.NewMeetingFake(),
		GitHub:    capabilities.NewGitHubFake(),
		Documents: capabilities.NewDocumentsFake(),
		GPU:       capabilities.NewGPUFake(),
		Nodes:     capabilities.NewNodeRegistry(),
	}
}

func (d *PersonalDeps) messaging() capabilities.Capability {
	if d != nil && d.Messaging != nil {
		return d.Messaging
	}
	return capabilities.NewMessagingFake()
}

func (d *PersonalDeps) workspace() capabilities.Capability {
	if d != nil && d.Workspace != nil {
		return d.Workspace
	}
	return capabilities.NewWorkspaceFake()
}

func (d *PersonalDeps) meetings() capabilities.Capability {
	if d != nil && d.Meetings != nil {
		return d.Meetings
	}
	return capabilities.NewMeetingFake()
}

func (d *PersonalDeps) github() capabilities.Capability {
	if d != nil && d.GitHub != nil {
		return d.GitHub
	}
	return capabilities.NewGitHubFake()
}

func (d *PersonalDeps) documents() capabilities.Capability {
	if d != nil && d.Documents != nil {
		return d.Documents
	}
	return capabilities.NewDocumentsFake()
}

func (d *PersonalDeps) gpu() capabilities.Capability {
	if d != nil && d.GPU != nil {
		return d.GPU
	}
	return capabilities.NewGPUFake()
}

func (d *PersonalDeps) nodes() *capabilities.NodeRegistry {
	if d != nil && d.Nodes != nil {
		return d.Nodes
	}
	return capabilities.NewNodeRegistry()
}

// RegisterPersonalDeferred converts every personal capability into
// deferred catalog tools with prefix-hash stability (Sections 13.3,
// 14.1). It returns the registered names in sorted order.
func RegisterPersonalDeferred(reg Registry, cat *DeferredCatalog, deps *PersonalDeps) ([]string, error) {
	if deps == nil {
		deps = PersonalFakes()
	}
	sources := []capabilities.Capability{
		deps.messaging(), deps.workspace(), deps.meetings(),
		deps.github(), deps.documents(), deps.gpu(), deps.nodes(),
	}
	names := []string{}
	for _, src := range sources {
		tools, err := ToolsFromCapability(capabilityShim{Capability: src})
		if err != nil {
			return nil, err
		}
		for _, t := range tools {
			def := t.Definition()
			// Only the Section 13.3 personal names register here;
			// sibling operations (store_install, meeting_transcribe)
			// stay reachable through their own wires.
			if !personalTool(def.Name) {
				continue
			}
			if err := reg.Register(t); err != nil {
				// An owner wave may have registered the name first;
				// the first registration wins and this one is skipped.
				if isDuplicate(err) {
					continue
				}
				return nil, err
			}
			// The tool is already in the registry above, so only
			// the deferred mark is left: deferred tools resolve
			// through search_tools from the next step and never
			// enter the fixed prefix (Sections 11.6, 13.3).
			if cat != nil {
				cat.MarkDeferred(def.Name)
			}
			names = append(names, def.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// capabilityShim adapts a capabilities.Capability to the invoker
// interface of the capability adapter.
type capabilityShim struct {
	capabilities.Capability
}

func (s capabilityShim) Manifest() capabilities.Manifest {
	return s.Capability.Manifest()
}

func (s capabilityShim) Invoke(operation string, args json.RawMessage, key string) (capabilities.TaskResult, error) {
	return s.Capability.Invoke(operation, args, key)
}

func (s capabilityShim) Poll(taskID string) (capabilities.TaskResult, error) {
	return s.Capability.Poll(taskID)
}

func (s capabilityShim) Cancel(taskID string) (capabilities.TaskResult, error) {
	return s.Capability.Cancel(taskID)
}

func (s capabilityShim) Update(taskID string, answer json.RawMessage) (capabilities.TaskResult, error) {
	return s.Capability.Update(taskID, answer)
}

// personalTool reports whether name is one of the eight personal
// deferred tools owned by this file.
func personalTool(name string) bool {
	switch name {
	case "email", "calendar", "contact_message", "meeting",
		"github", "userdb", "gpu_training", "nodes":
		return true
	}
	return false
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate")
}

// SettingsStore is the memory backend of the core settings tool
// (Section 13.2): read and change agent settings from the
// conversation. The master owns the tool; the store is the seam the
// daemon configuration replaces.
type SettingsStore struct {
	mu       sync.Mutex
	values   map[string]string
	versions map[string]int
}

// NewSettingsStore returns a settings store with defaults.
func NewSettingsStore() *SettingsStore {
	return &SettingsStore{
		values:   map[string]string{"timezone": "UTC", "language": "en"},
		versions: map[string]int{},
	}
}

// Get reads one setting.
func (s *SettingsStore) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[key]
	return v, ok
}

// Set changes one setting and bumps its version.
func (s *SettingsStore) Set(key, value string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	s.versions[key]++
	return s.versions[key]
}

// List returns every setting in sorted order.
func (s *SettingsStore) List() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

// Reminder is one stored reminder (Section 21.2): a moment or
// recurrence plus the preferred channel.
type Reminder struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	When    string `json:"when"`
	Repeat  string `json:"repeat,omitempty"`
	Channel string `json:"channel,omitempty"`
	Done    bool   `json:"done"`
	Snooze  string `json:"snooze,omitempty"`
}

// RemindersStore is the memory backend of the core reminders tool.
// The daemon replaces it with the agenda (Section 21.1); until then
// the tool stays functional behind the same provider shape.
type RemindersStore struct {
	mu    sync.Mutex
	items map[string]*Reminder
	seq   int
}

// NewRemindersStore returns an empty reminders store.
func NewRemindersStore() *RemindersStore {
	return &RemindersStore{items: map[string]*Reminder{}}
}

// Create adds a reminder.
func (s *RemindersStore) Create(text, when, repeat, channel string) *Reminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	r := &Reminder{ID: fmt.Sprintf("rem-%d", s.seq), Text: text, When: when, Repeat: repeat, Channel: channel}
	s.items[r.ID] = r
	return r
}

// List returns reminders in id order.
func (s *RemindersStore) List(includeDone bool) []*Reminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*Reminder{}
	for _, r := range s.items {
		if !includeDone && r.Done {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Snooze postpones a reminder.
func (s *RemindersStore) Snooze(id, when string) (*Reminder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	if !ok {
		return nil, false
	}
	r.Snooze = when
	return r, true
}

// Complete finishes a reminder.
func (s *RemindersStore) Complete(id string) (*Reminder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	if !ok {
		return nil, false
	}
	r.Done = true
	return r, true
}

// Delete removes a reminder.
func (s *RemindersStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return false
	}
	delete(s.items, id)
	return true
}

// PersonalCore wires the core settings and reminders stub providers
// to the memory stores. It only fills stubs whose provider is still
// unset, so the owning wave (agenda for reminders, master/config for
// settings) wins when it wires later or earlier.
func WirePersonalCore(reg *MapRegistry, settings *SettingsStore, reminders *RemindersStore) {
	if settings == nil {
		settings = NewSettingsStore()
	}
	if reminders == nil {
		reminders = NewRemindersStore()
	}
	fill := func(name string, provider func(ctx context.Context, inv Invocation) (Result, error)) {
		t, err := reg.Get(name)
		if err != nil {
			return
		}
		stub, ok := t.(*StubTool)
		if !ok || stub.Provider != nil {
			return
		}
		stub.Provider = provider
	}
	fill("settings", func(_ context.Context, inv Invocation) (Result, error) {
		var a struct {
			Action string `json:"action"`
			Key    string `json:"key"`
			Value  string `json:"value"`
		}
		if err := json.Unmarshal(inv.Arguments, &a); err != nil {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
		switch a.Action {
		case "", "list":
			raw, _ := json.Marshal(settings.List())
			return Result{Status: StatusOK, Output: string(raw)}, nil
		case "get":
			if a.Key == "" {
				return Result{Status: StatusInputError, Output: "key is required"}, nil
			}
			v, ok := settings.Get(a.Key)
			if !ok {
				return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown setting %q", a.Key)}, nil
			}
			return Result{Status: StatusOK, Output: v}, nil
		case "set":
			if a.Key == "" {
				return Result{Status: StatusInputError, Output: "key is required"}, nil
			}
			ver := settings.Set(a.Key, a.Value)
			return Result{Status: StatusOK, Output: fmt.Sprintf("set %s (rev %d)", a.Key, ver)}, nil
		default:
			return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown settings action %q", a.Action)}, nil
		}
	})
	fill("reminders", func(_ context.Context, inv Invocation) (Result, error) {
		var a struct {
			Action  string `json:"action"`
			ID      string `json:"id"`
			Text    string `json:"text"`
			When    string `json:"when"`
			Repeat  string `json:"repeat"`
			Channel string `json:"channel"`
			Done    bool   `json:"done"`
		}
		if err := json.Unmarshal(inv.Arguments, &a); err != nil {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
		switch a.Action {
		case "", "list":
			items := reminders.List(a.Done)
			raw, _ := json.Marshal(map[string]any{"reminders": items})
			return Result{Status: StatusOK, Output: string(raw)}, nil
		case "create":
			if a.Text == "" || a.When == "" {
				return Result{Status: StatusInputError, Output: "text and when are required"}, nil
			}
			r := reminders.Create(a.Text, a.When, a.Repeat, a.Channel)
			raw, _ := json.Marshal(r)
			return Result{Status: StatusOK, Output: string(raw)}, nil
		case "snooze":
			r, ok := reminders.Snooze(a.ID, a.When)
			if !ok {
				return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown reminder %q", a.ID)}, nil
			}
			raw, _ := json.Marshal(r)
			return Result{Status: StatusOK, Output: string(raw)}, nil
		case "complete":
			r, ok := reminders.Complete(a.ID)
			if !ok {
				return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown reminder %q", a.ID)}, nil
			}
			raw, _ := json.Marshal(r)
			return Result{Status: StatusOK, Output: string(raw)}, nil
		case "delete":
			if !reminders.Delete(a.ID) {
				return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown reminder %q", id(a.ID))}, nil
			}
			return Result{Status: StatusOK, Output: "deleted " + a.ID}, nil
		default:
			return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown reminders action %q", a.Action)}, nil
		}
	})
}

func id(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}
