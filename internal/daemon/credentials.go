package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

const credentialWaitTimeout = 10 * time.Minute

var credentialFields = map[string]bool{"username": true, "email": true, "password": true, "otp": true, "api_key": true}
var credentialHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*$`)

type credentialArgs struct {
	Site            string   `json:"site"`
	Purpose         string   `json:"purpose"`
	Fields          []string `json:"fields,omitempty"`
	RememberDefault bool     `json:"remember_default"`
}

func (a *credentialArgs) validate() error {
	a.Site = strings.ToLower(strings.TrimSpace(a.Site))
	if len(a.Site) > 253 || !(credentialHost.MatchString(a.Site) || net.ParseIP(a.Site) != nil) {
		return errors.New("site must be an exact host without a scheme, port, path or wildcard")
	}
	a.Purpose = strings.TrimSpace(a.Purpose)
	if len(a.Purpose) > 256 {
		return errors.New("purpose must be at most 256 bytes")
	}
	if a.Fields == nil {
		a.Fields = []string{"username", "password"}
	}
	if len(a.Fields) == 0 || len(a.Fields) > len(credentialFields) {
		return errors.New("request between one and five credential fields")
	}
	seen := map[string]bool{}
	for _, f := range a.Fields {
		if !credentialFields[f] || seen[f] {
			return errors.New("unknown or duplicate credential field")
		}
		seen[f] = true
	}
	return nil
}

type credentialOrigin struct {
	conversation, run, channel string
	requested                  func(api.CredentialsRequestedEvent)
}
type credentialOriginKey struct{}

type credentialResult struct {
	Status   string            `json:"status"`
	Refs     map[string]string `json:"refs,omitempty"`
	Guidance string            `json:"guidance,omitempty"`
}

type credentialWait struct {
	request  api.CredentialRequest
	run      string
	deadline time.Time
	done     chan struct{}
	result   credentialResult
}

// CredentialRegistry retains bounded request metadata across client reconnects.
// It never stores submitted values; only vault references reach waiting tools.
type CredentialRegistry struct {
	vault     *vault.Vault
	ephemeral map[string]ephemeralCredential
	stop      chan struct{}
	cleaning  bool
	mu        sync.Mutex
	pending   map[string]*credentialWait
	bus       *telemetry.Bus
	wait      time.Duration
	closed    bool
}

func newCredentialRegistry(bus *telemetry.Bus) *CredentialRegistry {
	return &CredentialRegistry{pending: map[string]*credentialWait{}, ephemeral: map[string]ephemeralCredential{}, stop: make(chan struct{}), bus: bus, wait: credentialWaitTimeout}
}

func (r *CredentialRegistry) Pending(conversation string) api.CredentialsPendingResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := api.CredentialsPendingResult{Requests: []api.CredentialRequest{}}
	for _, w := range r.pending {
		if conversation == "" || w.request.ConversationID == conversation {
			q := w.request
			q.Fields = append([]string{}, q.Fields...)
			out.Requests = append(out.Requests, q)
		}
	}
	sort.Slice(out.Requests, func(i, j int) bool { return out.Requests[i].CreatedAt < out.Requests[j].CreatedAt })
	return out
}

func (r *CredentialRegistry) request(ctx context.Context, a credentialArgs) (credentialResult, error) {
	if err := a.validate(); err != nil {
		return credentialResult{}, err
	}
	// Decide before remembered lookup, publication, callbacks or wait allocation.
	// A workspace's demo login must not reuse a user's saved localhost secret.
	if ownWorkspaceApp(ctx, a.Site) || autonomousDelivery(ctx) {
		guidance := autonomousPromptGuidance
		if ownWorkspaceApp(ctx, a.Site) {
			guidance = workspaceLoginGuidance
		}
		if err := recordPromptDenial(ctx, "request_credentials", guidance); err != nil {
			return credentialResult{}, err
		}
		return credentialResult{Status: "denied", Guidance: guidance}, nil
	}
	origin, _ := ctx.Value(credentialOriginKey{}).(credentialOrigin)
	if origin.conversation == "" || origin.run == "" {
		return credentialResult{}, errors.New("credential requests require an active conversation and run")
	}
	r.mu.Lock()
	if err := ctx.Err(); err != nil {
		r.mu.Unlock()
		return credentialResult{Status: "cancelled"}, nil
	}
	if !r.closed {
		if refs := r.rememberedLocked(ctx, a); refs != nil {
			r.mu.Unlock()
			return credentialResult{Status: "stored", Refs: refs}, nil
		}
	}
	count := 0
	for _, w := range r.pending {
		if w.request.ConversationID == origin.conversation {
			count++
		}
	}
	if r.closed || len(r.pending) >= 64 || count >= 8 {
		r.mu.Unlock()
		return credentialResult{}, errors.New("credential request capacity unavailable")
	}
	now := time.Now().UTC()
	w := &credentialWait{request: api.CredentialRequest{ID: string(types.NewMessageID()), ConversationID: origin.conversation, Site: a.Site, Purpose: a.Purpose, Fields: append([]string{}, a.Fields...), RememberDefault: a.RememberDefault, Status: "pending", CreatedAt: now.Format(time.RFC3339Nano)}, run: origin.run, deadline: now.Add(r.wait), done: make(chan struct{})}
	r.pending[w.request.ID] = w
	event := api.CredentialsRequestedEvent{RequestID: w.request.ID, ConversationID: origin.conversation, RunID: origin.run, Site: a.Site, Purpose: a.Purpose, Fields: append([]string{}, a.Fields...), RememberDefault: a.RememberDefault, CreatedAt: w.request.CreatedAt}
	if r.bus != nil {
		run := types.RunID(origin.run)
		_, _ = r.bus.Publish(api.EventCredentialsRequested, &run, nil, event)
	}
	r.mu.Unlock()
	if origin.requested != nil {
		origin.requested(event)
	}
	timer := time.NewTimer(time.Until(w.deadline))
	defer timer.Stop()
	select {
	case <-w.done:
	case <-ctx.Done():
		status := "cancelled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = "timeout"
		}
		r.mu.Lock()
		r.resolveLocked(w, status, nil)
		r.mu.Unlock()
	case <-timer.C:
		r.mu.Lock()
		r.resolveLocked(w, "timeout", nil)
		r.mu.Unlock()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return w.result, nil
}

func (r *CredentialRegistry) resolveLocked(w *credentialWait, status string, refs map[string]string) {
	if w.request.Status != "pending" {
		return
	}
	w.request.Status = status
	w.result = credentialResult{Status: status, Refs: refs}
	delete(r.pending, w.request.ID)
	if r.bus != nil {
		run := types.RunID(w.run)
		_, _ = r.bus.Publish(api.EventCredentialsResolved, &run, nil, api.CredentialsResolvedEvent{RequestID: w.request.ID, Status: status})
	}
	close(w.done)
}

func (r *CredentialRegistry) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.pending[id]
	if !ok {
		return &api.Error{Code: api.CodeUnknownTarget, Message: "credential request is no longer pending"}
	}
	r.resolveLocked(w, "cancelled", nil)
	return nil
}

func (r *CredentialRegistry) EndRun(run string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer r.cleanupLocked(run, time.Now())
	for _, w := range r.pending {
		if w.run == run {
			r.resolveLocked(w, "cancelled", nil)
		}
	}
}

func (r *CredentialRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	close(r.stop)
	r.closed = true
	r.cleanupLocked("", time.Now().Add(credentialLifetime))
	for _, w := range r.pending {
		r.resolveLocked(w, "cancelled", nil)
	}
}

type credentialsTool struct{ registry *CredentialRegistry }

func (*credentialsTool) Definition() tools.Definition {
	return tools.Definition{Name: "request_credentials", Description: "Request credentials through the secure web form; wait for references, never ask for values in chat. Reuse remembered exact-host credentials when available. Use returned secret:// refs with browser_act type. For your own workspace app, use the demo credentials you created directly with browser_act; autonomous delivery never opens a user form.", InputSchema: json.RawMessage(`{"type":"object","properties":{"site":{"type":"string","minLength":1,"maxLength":253,"description":"Exact page hostname only; omit scheme, path and port (for example 127.0.0.1)."},"purpose":{"type":"string","maxLength":256},"fields":{"type":"array","minItems":1,"maxItems":5,"uniqueItems":true,"items":{"type":"string","enum":["username","email","password","otp","api_key"]}},"remember_default":{"type":"boolean"}},"required":["site"],"additionalProperties":false}`), Effect: types.EffectCommunication, Idempotency: types.IdemKeyed, Cancel: types.CancelCooperative, Timeout: credentialWaitTimeout + 5*time.Second, OutputBudget: tools.DefaultOutputBudget, Platforms: []string{"linux"}, Profiles: []string{"default", "assistant"}, Group: "browser", Tags: []string{"credentials"}, Keywords: []string{"login", "sign in", "iniciar sesión", "contraseña", "password", "credentials"}}
}
func (t *credentialsTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var a credentialArgs
	if strictLibraryParams(inv.Arguments, &a) != nil {
		return tools.Result{Status: tools.StatusInputError, Output: "invalid credential request arguments"}, nil
	}
	if t.registry == nil {
		return tools.Result{Status: tools.StatusToolError, Output: "credential requests unavailable"}, nil
	}
	result, err := t.registry.request(ctx, a)
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	raw, _ := json.Marshal(result)
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, nil
}
func (*credentialsTool) ResourceKeys(json.RawMessage) ([]string, error) { return nil, nil }
