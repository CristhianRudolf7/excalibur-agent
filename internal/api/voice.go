package api

type VoiceTranscribeParams struct {
	AudioBlob string `json:"audio_blob"`
	MIME      string `json:"mime"`
	Language  string `json:"language,omitempty"`
}
type VoiceTranscribeResult struct {
	Text       string `json:"text"`
	Language   string `json:"language"`
	DurationMS int64  `json:"duration_ms"`
}
type VoiceSpeakParams struct {
	Text   string `json:"text"`
	Voice  string `json:"voice,omitempty"`
	Format string `json:"format,omitempty"`
}
type VoiceSpeakResult struct {
	AudioBlob  string `json:"audio_blob"`
	MIME       string `json:"mime"`
	DurationMS *int64 `json:"duration_ms,omitempty"`
}
type VoiceStatusResult struct {
	Available bool     `json:"available"`
	STTModel  string   `json:"stt_model"`
	TTSModel  string   `json:"tts_model"`
	Voices    []string `json:"voices"`
	Reason    string   `json:"reason,omitempty"`
}
