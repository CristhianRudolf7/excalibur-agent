package daemon

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

func TestCredentialTelegramLinkOnly(t *testing.T) {
	for _, channel := range []string{"telegram", "mail", "web", "api"} {
		t.Run(channel, func(t *testing.T) {
			t.Setenv("EXCALIBUR_WEB_BASE_URL", "https://assistant.example.test/app")
			b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, telemetry.NewBus(nil))
			a := &assistantTurns{backend: b}
			r := newCredentialRegistry(b.Bus)
			defer r.Close()
			ctx := context.WithValue(context.Background(), credentialOriginKey{}, a.credentialOrigin(context.Background(), "conversation", "run", channel))
			done := make(chan struct{})
			safego.Go("credential-channel-test", nil, func() {
				defer close(done)
				_, _ = r.request(ctx, credentialArgs{Site: "private.example.test", Purpose: "private purpose"})
			})
			deadline := time.Now().Add(time.Second)
			var requests []api.CredentialRequest
			for time.Now().Before(deadline) {
				requests = r.Pending("conversation").Requests
				history := b.Master.History("conversation", 0)
				if len(requests) == 1 && (channel == "web" || channel == "api" || len(history) == 1) {
					break
				}
				time.Sleep(time.Millisecond)
			}
			history := b.Master.History("conversation", 0)
			if channel == "web" || channel == "api" {
				if len(history) != 0 {
					t.Fatal("web request sent channel instruction")
				}
			} else {
				if len(history) != 1 {
					t.Fatal("channel did not receive assistant message")
				}
				text := history[0].Text
				if !strings.Contains(text, "https://assistant.example.test/app") || !strings.Contains(text, "secure credential card") {
					t.Fatalf("missing link: %s", text)
				}
				words := strings.Fields(text)
				link, err := url.Parse(words[1])
				if err != nil || len(requests) != 1 || link.Query().Get("credential") != requests[0].ID || link.Query().Get("conversation") != "conversation" {
					t.Fatal("channel link omitted request or conversation")
				}
				for _, private := range []string{"private.example.test", "private purpose", "secret://", "password", "username"} {
					if strings.Contains(text, private) {
						t.Errorf("channel contains %s", private)
					}
				}
			}
			if len(requests) != 1 {
				t.Fatal("request missing")
			}
			_ = r.Cancel(requests[0].ID)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("channel waiter leaked")
			}
		})
	}
}

func TestCredentialChannelFallback(t *testing.T) {
	for _, base := range []string{"", "not-a-url", "https://user:private@example.test", "https://example.test?token=private", "https://example.test/#private", "javascript:alert(1)"} {
		got := credentialWebInstruction(base, "request", "conversation")
		if got != "Open Excalibur web and use the secure credential card in this conversation." {
			t.Fatalf("unsafe URL instruction: %s", got)
		}
	}
}

func TestCredentialChannelLinkEscapesIdentifiers(t *testing.T) {
	text := credentialWebInstruction("https://assistant.example.test/app", "req&other=1", "conv space&x=1")
	link, err := url.Parse(strings.Fields(text)[1])
	if err != nil || link.Query().Get("credential") != "req&other=1" || link.Query().Get("conversation") != "conv space&x=1" || len(link.Query()) != 2 {
		t.Fatal("channel link did not encode identifiers")
	}
}

func TestPersonalAgentCredentialOriginAndUnread(t *testing.T) {
	ctx := context.Background()
	b, registry := personalBackend(t, nil)
	first := registry.Favorite()
	target, err := registry.Create(ctx, agents.CreateParams{Name: "Pablito"})
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.SetFavorite(ctx, target.ID); err != nil {
		t.Fatal(err)
	}
	a := &assistantTurns{backend: b}
	for _, test := range []struct{ origin, want string }{{first.ConversationID, first.ID}, {"", target.ID}} {
		origin := a.credentialOrigin(ctx, test.origin, "run", "web")
		agent, _ := registry.Get(test.want)
		origin.requested(api.CredentialsRequestedEvent{ConversationID: origin.conversation, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		got, _ := registry.Get(test.want)
		if got.Unread != agent.Unread+1 || got.LastPreview != "Secure credentials requested" || origin.conversation != got.ConversationID {
			t.Fatal("credential card missed its requesting agent")
		}
		if len(b.Master.History(got.ConversationID, 0)) != 0 {
			t.Fatal("credential card was persisted as a chat message")
		}
	}
}
