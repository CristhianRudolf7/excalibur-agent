package agents

import (
	"fmt"
	"regexp"
	"strings"
)

// AvatarSpec is the shared pixel portrait palette and sprite selection.
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

var avatarColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ValidateAvatar validates the shared contract until the avatar tool is integrated.
func ValidateAvatar(spec AvatarSpec) (AvatarSpec, error) {
	switch spec.Variant {
	case "hood", "long-hair", "cap", "bow", "beard", "headphones":
	default:
		return AvatarSpec{}, fmt.Errorf("invalid avatar variant")
	}
	switch spec.Accessory {
	case "none", "glasses", "sunglasses", "earring", "scarf", "star":
	default:
		return AvatarSpec{}, fmt.Errorf("invalid avatar accessory")
	}
	for _, color := range []*string{&spec.Tone, &spec.Hair, &spec.Highlight, &spec.Outfit, &spec.Accent, &spec.Background} {
		if !avatarColor.MatchString(*color) {
			return AvatarSpec{}, fmt.Errorf("avatar colors must be #RRGGBB")
		}
		*color = strings.ToUpper(*color)
	}
	return spec, nil
}

// DefaultAvatar returns a complete portrait for agents created without one.
func DefaultAvatar() AvatarSpec {
	return AvatarSpec{Variant: "hood", Tone: "#D5A57C", Hair: "#49342B", Highlight: "#F8DC91", Outfit: "#536FA6", Accent: "#C5A45A", Accessory: "none", Background: "#182338"}
}
