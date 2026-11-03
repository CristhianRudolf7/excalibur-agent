package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// AskImage uses the same bounded vision gateway as document OCR, with a caller
// prompt instead of a transcription prompt. Its caller controls feature gating;
// EXCALIBUR_VISION_OCR continues to apply only to document extraction.
func (b *ModelVisionBackend) AskImage(ctx context.Context, prompt, pngBase64 string) (string, string, error) {
	if len(prompt) > 16000 || len(pngBase64) > maxVisionImageBytes*4/3+4 {
		return "", b.model, fmt.Errorf("vision: prompt or image exceeds budget")
	}
	data, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		return "", b.model, fmt.Errorf("vision: invalid base64 PNG")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 100000000 {
		return "", b.model, fmt.Errorf("vision: invalid or oversized PNG")
	}
	ctx, cancel := context.WithTimeout(ctx, b.options.Timeout)
	defer cancel()
	resp, meta, err := b.gateway.Call(ctx, types.RoleVision, providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: prompt, Images: []providers.ImageInput{{URL: "data:image/png;base64," + pngBase64}}}}, MaxTokens: b.options.MaxTokens})
	b.mu.Lock()
	b.usage.Attempts += meta.Attempts
	b.usage.Tokens.Add(resp.Usage)
	b.usage.CostUSD += meta.CostUSD
	b.mu.Unlock()
	if err != nil {
		return "", b.model, fmt.Errorf("vision unavailable: %w", err)
	}
	text := strings.TrimSpace(resp.Message.Content)
	if text == "" || len(text) > 16384 {
		return "", b.model, fmt.Errorf("vision: empty or oversized model response")
	}
	return text, meta.Model, nil
}
