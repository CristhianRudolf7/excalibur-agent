package daemon

import (
	"context"
	"net/url"
	"os"
	"strings"

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
	origin := credentialOrigin{conversation: conv, run: run, channel: channel}
	switch strings.ToLower(channel) {
	case "", "api", "web":
		return origin
	}
	origin.requested = func(event api.CredentialsRequestedEvent) {
		// Channel adapters forward normal assistant messages. Keep site, purpose,
		// requested values and refs out of this message; the web card owns the form.
		if a.backend != nil && a.backend.Master != nil {
			_, _ = a.backend.Master.SendAssistant(ctx, conv, credentialWebInstruction(os.Getenv("EXCALIBUR_WEB_BASE_URL"), event.RequestID, event.ConversationID), nil)
		}
	}
	return origin
}
