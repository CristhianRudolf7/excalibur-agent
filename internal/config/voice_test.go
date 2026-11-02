package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVoiceConfigLayerAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "voice.toml")
	if err := os.WriteFile(path, []byte("[voice]\nprovider = 'openai'\nstt_model = 'custom-stt'\ntts_model = 'custom-tts'\nvoice = 'coral'\nenabled = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{SystemPath: path, UserPath: filepath.Join(t.TempDir(), "missing"), StartDir: t.TempDir(), NoEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Voice.Provider != "openai" || cfg.Voice.STTModel != "custom-stt" || cfg.Voice.TTSModel != "custom-tts" || cfg.Voice.Voice != "coral" || cfg.Voice.Enabled == nil || *cfg.Voice.Enabled {
		t.Fatalf("voice config %+v", cfg.Voice)
	}
	if err := WriteFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	again, err := Load(LoadOptions{SystemPath: path, UserPath: path, StartDir: t.TempDir(), NoEnv: true})
	if err != nil || again.Voice.Enabled == nil || *again.Voice.Enabled || again.Voice.STTModel != cfg.Voice.STTModel {
		t.Fatalf("roundtrip %+v %v", again.Voice, err)
	}
}
