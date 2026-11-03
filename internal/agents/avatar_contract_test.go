package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// Shared fixtures are also consumed by the web normalizer tests so persisted
// palettes produce the same face colors on both sides of the RPC boundary.
func TestAvatarSharedNormalizationContract(t *testing.T) {
	raw, err := os.ReadFile("testdata/avatar_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name            string
		Input, Expected AvatarSpec
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := ValidateAvatar(tc.Input)
			if err != nil || got != tc.Expected {
				t.Fatalf("got %+v, want %+v, err %v", got, tc.Expected, err)
			}
		})
	}
}

func TestAvatarContrastFallbackAcrossShades(t *testing.T) {
	for shade := 0; shade <= 255; shade++ {
		spec := DefaultAvatar("Contrast")
		spec.Tone = fmt.Sprintf("#%02x%02x%02x", shade, shade, shade)
		spec.Hair = spec.Tone
		got, err := ValidateAvatar(spec)
		if err != nil || avatarContrast(got.Tone, "#171923") < 3 || avatarContrast(got.Tone, got.Hair) < 1.5 {
			t.Fatalf("unreadable grayscale %d: %+v, %v", shade, got, err)
		}
	}
}
