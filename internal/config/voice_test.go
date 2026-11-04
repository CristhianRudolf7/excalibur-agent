package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVoiceConfigLayerAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "voice.toml")
	if err := os.WriteFile(path, []byte("[voice]\nprovider = 'openai'\nstt_model = 'custom-stt'\ntts_model = 'custom-tts'\nvoice = 'coral'\nlive_stt_model = 'custom-live-stt'\nlive_voice = 'cedar'\nenabled = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{SystemPath: path, UserPath: filepath.Join(t.TempDir(), "missing"), StartDir: t.TempDir(), NoEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Voice.LiveSTTModel != "custom-live-stt" || cfg.Voice.LiveVoice != "cedar" || cfg.Voice.Provider != "openai" || cfg.Voice.STTModel != "custom-stt" || cfg.Voice.TTSModel != "custom-tts" || cfg.Voice.Voice != "coral" || cfg.Voice.Enabled == nil || *cfg.Voice.Enabled {
		t.Fatalf("voice config %+v", cfg.Voice)
	}
	if err := WriteFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := Load(LoadOptions{SystemPath: path, UserPath: path, StartDir: t.TempDir(), NoEnv: true})
	if err != nil || again.Voice.Enabled == nil || *again.Voice.Enabled || again.Voice.STTModel != cfg.Voice.STTModel || again.Voice.LiveSTTModel != cfg.Voice.LiveSTTModel || again.Voice.LiveVoice != cfg.Voice.LiveVoice {
		t.Fatalf("roundtrip %+v %v", again.Voice, err)
	}
}

func TestLiveVoiceDefaults(t *testing.T) {
	cfg := Default().Voice
	if cfg.LiveSTTModel != "gpt-realtime-whisper" || cfg.LiveVoice != "marin" {
		t.Fatalf("live voice defaults: %+v", cfg)
	}
}
