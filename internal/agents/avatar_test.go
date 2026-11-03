package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateAvatarEnumsAndColors(t *testing.T) {
	for _, variant := range avatarVariants {
		for _, accessory := range avatarAccessories {
			spec := DefaultAvatar("Excalibur")
			spec.Variant, spec.Accessory = variant, accessory
			if got, err := ValidateAvatar(spec); err != nil || got != spec {
				t.Fatalf("%s/%s: got %+v, %v", variant, accessory, got, err)
			}
		}
	}
	for _, field := range []string{"variant", "accessory", "tone", "hair", "highlight", "outfit", "accent", "background"} {
		for _, invalid := range []string{"", "unknown", "#123", "#12345G", "red", "var(--skin)", "#12345678", " #123456"} {
			t.Run(field+"/"+invalid, func(t *testing.T) {
				raw, _ := json.Marshal(DefaultAvatar("Ada"))
				var fields map[string]string
				_ = json.Unmarshal(raw, &fields)
				fields[field] = invalid
				raw, _ = json.Marshal(fields)
				var spec AvatarSpec
				_ = json.Unmarshal(raw, &spec)
				if _, err := ValidateAvatar(spec); err == nil {
					t.Fatalf("accepted invalid %s", field)
				}
			})
		}
	}
}

func TestValidateAvatarNormalizesAndPreservesReadableFace(t *testing.T) {
	spec := DefaultAvatar("Ada")
	spec.Tone, spec.Hair = "#171923", "#f2c9a1"
	spec.Highlight, spec.Outfit, spec.Accent, spec.Background = "#abcdef", "#abcdef", "#abcdef", "#abcdef"
	got, err := ValidateAvatar(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got.Highlight != "#ABCDEF" || got.Outfit != "#ABCDEF" || got.Accent != "#ABCDEF" || got.Background != "#ABCDEF" {
		t.Fatalf("noncanonical colors: %+v", got)
	}
	if avatarContrast(got.Tone, "#171923") < 3 || avatarContrast(got.Hair, got.Tone) < 1.5 {
		t.Fatalf("unreadable face: %+v", got)
	}
	if again, err := ValidateAvatar(got); err != nil || again != got {
		t.Fatalf("normalization is not idempotent: %+v, %v", again, err)
	}
	// A valid darker skin color must survive normalization.
	spec.Tone, spec.Hair = "#B78059", "#30283F"
	got, err = ValidateAvatar(spec)
	if err != nil || got.Tone != spec.Tone || got.Hair != spec.Hair {
		t.Fatalf("changed readable skin or hair: %+v, %v", got, err)
	}
}

func TestDefaultAvatarDeterminism(t *testing.T) {
	seen := map[AvatarSpec]bool{}
	for _, name := range []string{"", "Ada", "Merlin", "Robot", "Grace", "Lin", "Sam"} {
		got := DefaultAvatar(name)
		if got != DefaultAvatar(name) || got != DefaultAvatar("  "+strings.ToUpper(name)+"  ") {
			t.Fatalf("unstable default for %q", name)
		}
		if valid, err := ValidateAvatar(got); err != nil || valid != got {
			t.Fatalf("invalid default for %q: %+v, %v", name, got, err)
		}
		seen[got] = true
	}
	if len(seen) < 5 {
		t.Fatal("names should produce varied palettes and silhouettes")
	}
}
