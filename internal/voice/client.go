// Package voice implements bounded OpenAI audio operations independently of model tools.
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
)

const MaxAudioBytes = 25 * 1024 * 1024
const MaxTextCharacters = 4096

// Credentials is invoked at each operation boundary; keys never enter errors.
type Credentials func(context.Context) (baseURL, key string, err error)

// ModelCredentials uses the same layered settings and registry resolution as chat.
func ModelCredentials(cfg config.ModelsConfig, resolve registry.SecretResolver) Credentials {
	return func(ctx context.Context) (string, string, error) {
		s, err := models.ResolveModelSettings(cfg, nil)
		if err != nil {
			return "", "", errors.New("voice: credentials unavailable")
		}
		if s.Provider != "" && s.Provider != registry.KindOpenAI {
			return "", "", errors.New("voice: OpenAI provider is not configured")
		}
		def := s.ProviderDef()
		def.Kind = registry.KindOpenAI
		key, _, err := registry.ResolveKey(ctx, def, resolve)
		if err != nil || key == "" {
			return "", "", errors.New("voice: OpenAI credential unavailable")
		}
		base := s.BaseURL
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		return base, key, nil
	}
}

// Client bounds requests, response bodies and elapsed time, including injected transports.
type Client struct {
	Config      config.VoiceConfig
	Credentials Credentials
	HTTP        *http.Client
}

func (c *Client) settings() config.VoiceConfig {
	cfg := c.Config
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.STTModel == "" {
		cfg.STTModel = "gpt-4o-mini-transcribe"
	}
	if cfg.TTSModel == "" {
		cfg.TTSModel = "gpt-4o-mini-tts"
	}
	if cfg.Voice == "" {
		cfg.Voice = "alloy"
	}
	return cfg
}

func (c *Client) credentials(ctx context.Context) (string, string, error) {
	cfg := c.settings()
	if cfg.Enabled != nil && !*cfg.Enabled {
		return "", "", errors.New("voice: disabled")
	}
	if cfg.Provider != "openai" {
		return "", "", errors.New("voice: unsupported provider")
	}
	if c.Credentials == nil {
		return "", "", errors.New("voice: OpenAI credential unavailable")
	}
	base, key, err := c.Credentials(ctx)
	if err != nil || key == "" {
		return "", "", errors.New("voice: OpenAI credential unavailable")
	}
	return strings.TrimRight(base, "/"), key, nil
}

// Status resolves availability without making a network request.
func (c *Client) Status(ctx context.Context) api.VoiceStatusResult {
	cfg := c.settings()
	s := api.VoiceStatusResult{STTModel: cfg.STTModel, TTSModel: cfg.TTSModel, Voices: []string{"alloy", "ash", "ballad", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer", "verse", "marin", "cedar"}}
	_, _, err := c.credentials(ctx)
	s.Available = err == nil
	if err != nil {
		s.Reason = err.Error()
	}
	return s
}

func invalid(message string) error { return &api.Error{Code: api.CodeInvalidParams, Message: message} }

// Transcribe sends multipart audio, using JSON (supported by mini-transcribe).
// Duration and detected language are returned only when supplied by the provider;
// otherwise duration_ms is zero and language is the requested language (or empty).
func (c *Client) Transcribe(ctx context.Context, p api.VoiceTranscribeParams, audio []byte) (api.VoiceTranscribeResult, error) {
	var out api.VoiceTranscribeResult
	if len(audio) == 0 || len(audio) > MaxAudioBytes {
		return out, invalid("voice: audio must be nonempty and at most 25 MB")
	}
	media, _, err := mime.ParseMediaType(p.MIME)
	extensions := map[string]string{"audio/mpeg": "mp3", "audio/mp3": "mp3", "audio/mp4": "mp4", "audio/m4a": "m4a", "audio/wav": "wav", "audio/x-wav": "wav", "audio/webm": "webm", "audio/ogg": "ogg", "audio/opus": "ogg", "audio/flac": "flac", "video/webm": "webm", "video/mp4": "mp4"}
	ext := extensions[media]
	if err != nil || ext == "" {
		return out, invalid("voice: unsupported audio MIME")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", c.settings().STTModel)
	_ = writer.WriteField("response_format", "json")
	if p.Language != "" {
		_ = writer.WriteField("language", p.Language)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="audio.%s"`, ext))
	h.Set("Content-Type", media)
	part, err := writer.CreatePart(h)
	if err != nil {
		return out, errors.New("voice: audio encoding failed")
	}
	_, _ = part.Write(audio)
	_ = writer.Close()
	data, err := c.request(ctx, "/audio/transcriptions", writer.FormDataContentType(), body.Bytes(), 1<<20)
	if err != nil {
		return out, err
	}
	var result struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if json.Unmarshal(data, &result) != nil {
		return out, errors.New("voice: invalid transcription response")
	}
	out.Text, out.Language, out.DurationMS = result.Text, result.Language, int64(result.Duration*1000)
	if out.Language == "" {
		out.Language = p.Language
	}
	return out, nil
}

// Speak accepts at most 4096 Unicode characters. Clients split longer text.
func (c *Client) Speak(ctx context.Context, p api.VoiceSpeakParams) ([]byte, string, error) {
	if !utf8.ValidString(p.Text) || strings.TrimSpace(p.Text) == "" || utf8.RuneCountInString(p.Text) > MaxTextCharacters {
		return nil, "", invalid("voice: text must contain 1 to 4096 characters; split longer text into sentences")
	}
	if p.Format == "" {
		p.Format = "mp3"
	}
	media := map[string]string{"mp3": "audio/mpeg", "opus": "audio/ogg", "wav": "audio/wav"}[p.Format]
	if media == "" {
		return nil, "", invalid("voice: format must be mp3, opus or wav")
	}
	if p.Voice == "" {
		p.Voice = c.settings().Voice
	}
	body, _ := json.Marshal(map[string]string{"model": c.settings().TTSModel, "input": p.Text, "voice": p.Voice, "response_format": p.Format})
	data, err := c.request(ctx, "/audio/speech", "application/json", body, MaxAudioBytes)
	return data, media, err
}

func (c *Client) request(ctx context.Context, path, contentType string, body []byte, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	base, key, err := c.credentials(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("voice: invalid endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", contentType)
	client := &http.Client{Timeout: 90 * time.Second}
	if c.HTTP != nil {
		*client = *c.HTTP
	}
	// Never forward credentials to a redirect target.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("voice: audio request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("voice: audio provider returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("voice: audio response failed")
	}
	if len(data) == 0 || int64(len(data)) > limit {
		return nil, errors.New("voice: invalid audio response size")
	}
	return data, nil
}
