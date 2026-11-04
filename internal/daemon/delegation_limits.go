package daemon

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// This is a generous emergency step guard, not a delivery deadline. Wall and
// cost pressure are enabled only by a textual user limit, never by a card.
const defaultDeliverySteps = 10000

var userTimeLimit = regexp.MustCompile(`\b(?:en|in|within|at most|no mas de|maximo)\s+(\d+(?:\.\d+)?)\s*(minutos?|minutes?|mins?|horas?|hours?|seconds?|segundos?)\b`)
var userCostLimit = regexp.MustCompile(`\b(?:no gastes mas de|do not spend more than|spend at most|budget|presupuesto)(?:\s+of|\s+de)?\s*\$?\s*(\d+(?:\.\d+)?)\b`)
var hurryInstruction = regexp.MustCompile(`\b(?:hurry|rush|wrap up|finish (?:and report |up |the task )?(?:now|immediately|asap)|deliver (?:now|immediately|asap)|finalize now|report now|stop (?:now|immediately|work|expanding|working|coding|making|adding)|no more (?:changes|work)|time (?:is up|pressure)|remaining (?:time|budget)|budget (?:is|left)|finaliza|termina ya|termina ahora|termina y reporta|acaba ya|reporta ahora|sin mas cambios|apresurate|date prisa|deja de (?:trabajar|programar|ampliar)|deten|detente)\b`)

func textualDeliveryLimits(request string) (int64, float64) {
	text := foldAccents(strings.ToLower(request))
	var seconds int64
	var cost float64
	if match := userTimeLimit.FindStringSubmatch(text); match != nil {
		value, _ := strconv.ParseFloat(match[1], 64)
		if strings.HasPrefix(match[2], "h") {
			value *= 3600
		} else if strings.HasPrefix(match[2], "m") {
			value *= 60
		}
		if value > 0 && value < 365*24*3600 {
			seconds = int64(value)
		}
	}
	if match := userCostLimit.FindStringSubmatch(text); match != nil {
		cost, _ = strconv.ParseFloat(match[1], 64)
	}
	return seconds, cost
}

func (p deliveryPolicy) budget() types.Budget {
	return types.Budget{MaxSteps: defaultDeliverySteps, MaxSeconds: p.MaxSeconds, MaxCostUSD: p.MaxCostUSD}
}

func (p deliveryPolicy) prepareDelegation(args json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return nil, err
	}
	var action, text string
	_ = json.Unmarshal(fields["action"], &action)
	_ = json.Unmarshal(fields["text"], &text)
	if (action == "message" || action == "redirect") && p.MaxSeconds == 0 && p.MaxCostUSD == 0 && hurryInstruction.MatchString(foldAccents(strings.ToLower(text))) {
		return nil, fmt.Errorf("message dropped: the user set no time or cost limit; let the child complete and verify the full spec, then await its result")
	}
	if action != "spawn" {
		return args, nil
	}
	var card map[string]json.RawMessage
	if err := json.Unmarshal(fields["card"], &card); err != nil || card == nil {
		return nil, fmt.Errorf("delegation requires a card")
	}
	card["budget"], _ = json.Marshal(p.budget())
	// Deadlines written by a coordinator are not user authorization.
	delete(card, "deadline")
	fields["card"], _ = json.Marshal(card)
	return json.Marshal(fields)
}
