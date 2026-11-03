package daemon

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

func credentialWebInstruction(base, request, conversation string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" {
		query := u.Query()
		query.Set("credential", request)
		query.Set("conversation", conversation)
		u.RawQuery = query.Encode()
		return "Open " + u.String() + " and use the secure credential card in this conversation."
	}
	return "Open Excalibur web and use the secure credential card in this conversation."
}

func (a *assistantTurns) credentialOrigin(ctx context.Context, conv, run, channel string) credentialOrigin {
	if conv == "" && a.backend != nil {
		conv = a.backend.SystemConversation("")
	}
	origin := credentialOrigin{conversation: conv, run: run, channel: channel}
	origin.requested = func(event api.CredentialsRequestedEvent) {
		// A card is an unread chat item even though its values never enter history.
		if a.backend != nil {
			if registry := a.backend.agentRegistry(); registry != nil {
				at, _ := time.Parse(time.RFC3339Nano, event.CreatedAt)
				if err := registry.ObserveMessage(ctx, conv, "Secure credentials requested", at, true); err != nil {
					slog.Error("could not update credential request unread state", "error", err)
				}
			}
		}
		switch strings.ToLower(channel) {
		case "", "api", "web":
			return
		}
		// Channel adapters forward normal assistant messages. Keep site, purpose,
		// requested values and refs out of this message; the web card owns the form.
		if a.backend != nil && a.backend.Master != nil {
			_, _ = a.backend.Master.SendAssistant(ctx, conv, credentialWebInstruction(os.Getenv("EXCALIBUR_WEB_BASE_URL"), event.RequestID, event.ConversationID), nil)
		}
	}
	return origin
}
