package agents

// AvatarSpec is the portable pixel-art palette and silhouette for a personal agent.
// Tone colors skin; outfit and accent color clothing and accessory pixels.
type AvatarSpec struct {
	Variant    string `json:"variant"`
	Tone       string `json:"tone"`
	Hair       string `json:"hair"`
	Highlight  string `json:"highlight"`
	Outfit     string `json:"outfit"`
	Accent     string `json:"accent"`
	Accessory  string `json:"accessory"`
	Background string `json:"background"`
}
