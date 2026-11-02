package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/voice"
)

// VoiceRoutes shares the gateway's content-addressed blob store.
type VoiceRoutes struct {
	Client *voice.Client
	Blobs  gateway.BlobStore
}

func (r *VoiceRoutes) Op(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	client := &voice.Client{}
	if r != nil && r.Client != nil {
		client = r.Client
	}
	if method == api.MethodVoiceStatus {
		return client.Status(ctx), nil
	}
	if r == nil || r.Blobs == nil {
		return nil, &api.Error{Code: api.CodeAppError, Message: "voice: service unavailable"}
	}
	switch method {
	case api.MethodVoiceTranscribe:
		var p api.VoiceTranscribeParams
		if json.Unmarshal(raw, &p) != nil || !audioDigest(p.AudioBlob) {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "voice: invalid audio blob"}
		}
		rc, size, err := r.Blobs.Get(p.AudioBlob)
		if err != nil {
			return nil, &api.Error{Code: api.CodeUnknownTarget, Message: "voice: audio blob not found"}
		}
		defer rc.Close()
		if size > voice.MaxAudioBytes {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "voice: audio exceeds 25 MB"}
		}
		audio, err := io.ReadAll(io.LimitReader(rc, voice.MaxAudioBytes+1))
		if err != nil {
			return nil, &api.Error{Code: api.CodeAppError, Message: "voice: cannot read audio blob"}
		}
		return client.Transcribe(ctx, p, audio)
	case api.MethodVoiceSpeak:
		var p api.VoiceSpeakParams
		if json.Unmarshal(raw, &p) != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "voice: invalid speak params"}
		}
		data, media, err := client.Speak(ctx, p)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(data)
		digest := hex.EncodeToString(hash[:])
		if err := r.Blobs.Put(digest, bytes.NewReader(data)); err != nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: "voice: cannot store speech"}
		}
		return api.VoiceSpeakResult{AudioBlob: digest, MIME: media}, nil
	}
	return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown voice method"}
}

func audioDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
