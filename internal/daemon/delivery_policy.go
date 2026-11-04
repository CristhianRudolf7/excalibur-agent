package daemon

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
)

// deliveryPolicy is captured from user text before delegation. Neither model
// arguments nor attachment contents can authorize public network exposure.
type deliveryPolicy struct {
	apps          *workspaceApps
	Deadline      time.Time
	Autonomous    bool
	PublicPreview bool
	MaxSeconds    int64
	MaxCostUSD    float64
}
type deliveryPolicyKey struct{}

func withDeliveryPolicy(ctx context.Context, policy deliveryPolicy) context.Context {
	return context.WithValue(ctx, deliveryPolicyKey{}, policy)
}

var previewRequestPattern = regexp.MustCompile(`\b(public (?:preview|link|url)|(?:link|url|enlace) (?:de |con )?cloudflare|cloudflare (?:link|url)|enlace publico|vista publica)\b`)
var previewNegationPattern = regexp.MustCompile(`\b(?:no|not|never|don't|do not|sin|without)\s+(?:(?:me|lo|aun|todavia)\s+)*(?:publish|publishing|publicar|publiques|publique|compartir|compartas|share|sharing|give.{0,20}(?:public|cloudflare)|(?:un\s+)?enlace\s+publico|public\s+(?:link|url|preview))\b`)
var localOnlyPreview = regexp.MustCompile(`\b(?:local(?:host)? only|only local(?:host)?|solo (?:en )?local(?:host)?|sin (?:publicacion|publicar)|without publishing)\b`)

var previewPublishCommand = regexp.MustCompile(`\b(?:publish|publicar|publicalo|publicala|comparte|compartir|share (?:it|the app|the site))\b`)
var previewAsk = regexp.MustCompile(`\b(?:give|send|provide|get|want|need|create|generate|show|include|share|with|please|build|pasame|dame|quiero|necesito|enviame|pasa|pon|incluye|entrega|programalo)\b`)
var previewInformation = regexp.MustCompile(`^(?:(?:can|could) you |please )?(?:what is|how does|explain|describe|que es|como funciona)\b`)

func explicitPublicPreviewRequest(request string) bool {
	text := strings.TrimSpace(foldAccents(strings.ToLower(request)))
	// Conservative denial wins within the request. A subsequent explicit turn
	// can authorize publishing; incidental mentions and questions cannot.
	if previewNegationPattern.MatchString(text) || localOnlyPreview.MatchString(text) || previewInformation.MatchString(text) {
		return false
	}
	if previewPublishCommand.MatchString(text) {
		return true
	}
	match := previewRequestPattern.FindString(text)
	return match != "" && (previewAsk.MatchString(text) || strings.Trim(text, " .?!¿¡") == match)
}

// Look only at the current delivery's recent user turns, stopping at its
// attachment boundary. Later denials override earlier requests. Assistant text
// and document bodies never grant authorization.
func deliveryPolicyForTurns(history []master.Normalized, msg master.Normalized) deliveryPolicy {
	policy := deliveryPolicy{apps: &workspaceApps{}}
	if msg.RelayedBy != "" || msg.Role != "" && msg.Role != "user" {
		return policy
	}
	if !specBuildCommand.MatchString(foldAccents(strings.ToLower(msg.Text))) {
		history = nil
	}
	rows := append(append([]master.Normalized(nil), history...), msg)
	previewDecided := false
	turns := 0
	for i := len(rows) - 1; i >= 0 && turns < delegationAttachmentTurns; i-- {
		row := rows[i]
		if row.RelayedBy != "" || row.Role != "" && row.Role != "user" {
			continue
		}
		turns++
		text := foldAccents(strings.ToLower(row.Text))
		if !previewDecided {
			if previewNegationPattern.MatchString(text) || localOnlyPreview.MatchString(text) {
				previewDecided = true
			} else if explicitPublicPreviewRequest(row.Text) {
				policy.PublicPreview = true
				previewDecided = true
			}
		}
		seconds, cost := textualDeliveryLimits(row.Text)
		if policy.MaxSeconds == 0 {
			policy.MaxSeconds = seconds
		}
		if policy.MaxCostUSD == 0 {
			policy.MaxCostUSD = cost
		}
		if len(delegationDocumentIDs([]master.Normalized{row}, "")) > 0 {
			break
		}
	}
	if policy.MaxSeconds > 0 {
		policy.Deadline = time.Now().Add(time.Duration(policy.MaxSeconds) * time.Second)
	}
	return policy
}
