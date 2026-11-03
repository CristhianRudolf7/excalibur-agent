package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func startCredentialRequest(t *testing.T, r *CredentialRegistry, ctx context.Context, args credentialArgs) (api.CredentialsRequestedEvent, <-chan credentialResult) {
	t.Helper()
	events := make(chan api.CredentialsRequestedEvent, 1)
	ctx = context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{conversation: "conversation", run: "run", requested: func(e api.CredentialsRequestedEvent) { events <- e }})
	result := make(chan credentialResult, 1)
	safego.Go("credential-test", nil, func() {
		out, err := r.request(ctx, args)
		if err != nil {
			t.Error(err)
		}
		result <- out
	})
	select {
	case event := <-events:
		return event, result
	case <-time.After(time.Second):
		t.Fatal("no requested event")
		return api.CredentialsRequestedEvent{}, result
	}
}

func TestCredentialWaitReloadCancelTimeout(t *testing.T) {
	for _, mode := range []string{"cancel", "context", "timeout", "end_run", "close"} {
		t.Run(mode, func(t *testing.T) {
			bus := telemetry.NewBus(nil)
			r := newCredentialRegistry(bus)
			r.wait = 50 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			event, result := startCredentialRequest(t, r, ctx, credentialArgs{Site: "Example.com", Purpose: "Sign in"})
			for i := 0; i < 2; i++ {
				p := r.Pending("conversation")
				if len(p.Requests) != 1 || p.Requests[0].Site != "example.com" || len(p.Requests[0].Fields) != 2 {
					t.Fatalf("reload: %+v", p)
				}
			}
			if len(r.Pending("other").Requests) != 0 {
				t.Fatal("cross-conversation request exposed")
			}
			want := "cancelled"
			switch mode {
			case "cancel":
				if err := r.Cancel(event.RequestID); err != nil {
					t.Fatal(err)
				}
			case "context":
				cancel()
			case "end_run":
				r.EndRun("run")
			case "close":
				r.Close()
			case "timeout":
				want = "timeout"
			}
			select {
			case out := <-result:
				if out.Status != want || out.Refs != nil {
					t.Fatalf("result: %+v", out)
				}
			case <-time.After(time.Second):
				t.Fatal("request did not settle")
			}
			if len(r.Pending("conversation").Requests) != 0 {
				t.Fatal("resolved request retained")
			}
			events := bus.Replay(types.Cursor{})
			if len(events) != 2 || events[0].Type != api.EventCredentialsRequested || events[1].Type != api.EventCredentialsResolved {
				t.Fatalf("events: %+v", events)
			}
			if err := r.Cancel(event.RequestID); err == nil {
				t.Fatal("late cancel accepted")
			}
		})
	}
}

func TestCredentialValidationAndCapacity(t *testing.T) {
	for _, site := range []string{"", "https://example.com", "*.example.com", "example.com/path", "example.com:443", "user@example.com", "example..com", "example.com?x=y"} {
		a := credentialArgs{Site: site}
		if a.validate() == nil {
			t.Errorf("invalid host accepted: %q", site)
		}
	}
	for _, fields := range [][]string{{}, {"username", "username"}, {"unknown"}} {
		a := credentialArgs{Site: "example.com", Fields: fields}
		if a.validate() == nil {
			t.Errorf("invalid fields accepted: %v", fields)
		}
	}
	r := newCredentialRegistry(nil)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		_, result := startCredentialRequest(t, r, ctx, credentialArgs{Site: "example.com"})
		t.Cleanup(func() {
			select {
			case <-result:
			case <-time.After(time.Second):
				t.Error("waiter leaked")
			}
		})
	}
	origin := context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{conversation: "conversation", run: "run"})
	if _, err := r.request(origin, credentialArgs{Site: "example.com"}); err == nil {
		t.Fatal("conversation bound ignored")
	}
	r.Close()
}

func TestCredentialToolContract(t *testing.T) {
	d := (&credentialsTool{}).Definition()
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(d.Profiles, ","), "eval") {
		t.Fatal("credential tool in eval")
	}
	cat := tools.NewDeferredCatalog(tools.NewMapRegistry())
	if err := cat.RegisterDeferred(&credentialsTool{}); err != nil {
		t.Fatal(err)
	}
	for _, intent := range []string{"login", "sign in", "iniciar sesión", "contraseña", "password", "credentials"} {
		if len(cat.Suggest(intent)) != 1 {
			t.Errorf("missing intent %q", intent)
		}
	}
	out, _ := (&credentialsTool{}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"site":"example.com","values":{"password":"sentinel"}}`)})
	if out.Status != tools.StatusInputError || strings.Contains(out.Output, "sentinel") {
		t.Fatalf("unsafe args: %+v", out)
	}
}

func TestCredentialGlobalCapacity(t *testing.T) {
	r := newCredentialRegistry(nil)
	for i := 0; i < 64; i++ {
		id := string(rune('a' + i))
		r.pending[id] = &credentialWait{request: api.CredentialRequest{ID: id, Status: "pending", ConversationID: id}, done: make(chan struct{})}
	}
	ctx := context.WithValue(context.Background(), credentialOriginKey{}, credentialOrigin{conversation: "fresh-conversation", run: "run"})
	if _, err := r.request(ctx, credentialArgs{Site: "example.com"}); err == nil {
		t.Fatal("global request bound ignored")
	}
	r.Close()
}
