package agents

import (
	"crypto/sha256"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var avatarHex = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var avatarVariants = []string{"hood", "long-hair", "cap", "bow", "beard", "headphones"}
var avatarAccessories = []string{"none", "glasses", "sunglasses", "earring", "scarf", "star"}

// ValidateAvatar rejects unsupported silhouettes, accessories and colors, and
// returns canonical uppercase hex colors. Very dark skin falls back to warm
// skin so the fixed dark eyes remain readable (contrast ratio at least 3:1).
// Hair too close to skin receives a contrasting fallback (at least 1.5:1).
func ValidateAvatar(spec AvatarSpec) (AvatarSpec, error) {
	validEnum := func(value string, values []string) bool {
		for _, allowed := range values {
			if value == allowed {
				return true
			}
		}
		return false
	}
	if !validEnum(spec.Variant, avatarVariants) {
		return AvatarSpec{}, fmt.Errorf("avatar: unsupported variant %q", spec.Variant)
	}
	if !validEnum(spec.Accessory, avatarAccessories) {
		return AvatarSpec{}, fmt.Errorf("avatar: unsupported accessory %q", spec.Accessory)
	}
	for _, field := range []struct {
		name  string
		value *string
	}{{"tone", &spec.Tone}, {"hair", &spec.Hair}, {"highlight", &spec.Highlight},
		{"outfit", &spec.Outfit}, {"accent", &spec.Accent}, {"background", &spec.Background}} {
		if !avatarHex.MatchString(*field.value) {
			return AvatarSpec{}, fmt.Errorf("avatar: %s must be a #RRGGBB color", field.name)
		}
		*field.value = strings.ToUpper(*field.value)
	}
	if avatarContrast(spec.Tone, "#171923") < 3 {
		spec.Tone = "#F2C9A1"
	}
	if avatarContrast(spec.Hair, spec.Tone) < 1.5 {
		spec.Hair = "#30283F"
		if avatarContrast(spec.Hair, spec.Tone) < 1.5 {
			spec.Hair = "#FFF1D6"
		}
	}
	return spec, nil
}

func avatarContrast(a, b string) float64 {
	luminance := func(color string) float64 {
		var channels [3]float64
		for i := range channels {
			v, _ := strconv.ParseUint(color[1+i*2:3+i*2], 16, 8)
			c := float64(v) / 255
			if c <= 0.04045 {
				channels[i] = c / 12.92
			} else {
				channels[i] = math.Pow((c+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
	}
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

// DefaultAvatar derives a stable, coordinated palette from a name without RNG
// state or platform dependencies. It also accepts an empty seed for first boot.
func DefaultAvatar(seed string) AvatarSpec {
	hash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(seed))))
	palettes := [][4]string{
		{"#586FA8", "#D5E2FF", "#F4C76B", "#E6ECF8"},
		{"#7961A8", "#E5D8FF", "#EDB3CE", "#F0E9FA"},
		{"#438579", "#C5EADF", "#EFC478", "#E3F1EB"},
		{"#B9694F", "#FFD9B5", "#80C7C2", "#F8E9DE"},
		{"#9A6481", "#F7D5E9", "#B9D788", "#F5E8EE"},
		{"#577F9C", "#D5EEFA", "#E9BB8B", "#E7EFF5"},
	}
	p := palettes[int(hash[1])%len(palettes)]
	tones := []string{"#F2C9A1", "#DCA879", "#B78059", "#F6D8BF"}
	hair := []string{"#30283F", "#543B32", "#725280", "#84613F"}
	spec, _ := ValidateAvatar(AvatarSpec{
		Variant: avatarVariants[int(hash[0])%len(avatarVariants)],
		Tone:    tones[int(hash[2])%len(tones)], Hair: hair[int(hash[3])%len(hair)],
		Outfit: p[0], Highlight: p[1], Accent: p[2], Background: p[3], Accessory: "none",
	})
	return spec
}
