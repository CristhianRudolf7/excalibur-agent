package openai

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestServiceTierFromEnv(t *testing.T) {
	c := &respClient{}
	req := providers.ChatRequest{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}}}
	body, err := c.buildBody(req, false)
	if err != nil || body.ServiceTier != "" {
		t.Fatalf("unset env must send no tier: %v %q", err, body.ServiceTier)
	}
	t.Setenv(ServiceTierEnv, "flex")
	body, err = c.buildBody(req, false)
	if err != nil || body.ServiceTier != "flex" {
		t.Fatalf("tier = %q, err %v", body.ServiceTier, err)
	}
}
