package tools

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"io"
	"os"
	"strings"
	"testing"
)

func emulatorScreen(t *testing.T, name string) (image.Image, string) {
	t.Helper()
	const root = "testdata/screen/"
	f, err := os.Open(root + name + ".ppm.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	data, err := io.ReadAll(io.LimitReader(z, 2<<20))
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile(root + "metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Hashes map[string]string `json:"ppm_sha256"`
	}
	if err := json.Unmarshal(metadata, &manifest); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != manifest.Hashes[name] {
		t.Fatal("fixture checksum differs from captured raster")
	}
	img, err := decodePNM(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Size() != image.Pt(720, 400) {
		t.Fatal(img.Bounds())
	}
	expected, err := os.ReadFile(root + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return img, strings.TrimSuffix(string(expected), "\n")
}

func TestInspectScreenEmulatorFixtures(t *testing.T) {
	for _, name := range []string{"mixed", "no-disk"} {
		t.Run(name, func(t *testing.T) {
			img, want := emulatorScreen(t, name)
			out, err := inspectScreenText(context.Background(), img)
			if err != nil {
				t.Fatal(err)
			}
			_, got, _ := strings.Cut(out, "screen_text:\n")
			got = strings.TrimSuffix(got, "\n")
			errors := screenTextDistance([]rune(want), []rune(got))
			t.Logf("%d errors / %d characters (%.2f%% accuracy)", errors, len([]rune(want)), 100*(1-float64(errors)/float64(len([]rune(want)))))
			// Exact recovery is stronger than the allowed one-error-per-200 bound.
			// Check layout as well as letters, including leading inverse spaces.
			if got != want {
				t.Fatalf("fixture text differs:\ngot:  %q\nwant: %q", got, want)
			}
			if !strings.Contains(out, "screen_text_grid: 80x25\n") || !strings.Contains(out, "screen_text_line_confidence:") {
				t.Fatal(out)
			}

			// Exercise automatic reading through the public tool path. All decoding,
			// matching and report ordering stays local, without OCR or a provider.
			root := t.TempDir()
			imageFixture(t, root, "screen.png", img)
			tool := &InspectImageTool{Deps: &FileDeps{Workdir: root}}
			t.Setenv("EXCALIBUR_INSPECT_IMAGE", "1")
			result := fileCall(t, tool, map[string]any{"path": "screen.png"})
			requireImageOutput(t, result, out)
			if strings.Index(result.Output, "screen_text:") > strings.Index(result.Output, "ASCII (") {
				t.Fatal("screen text lost report priority")
			}
		})
	}
}

// Levenshtein distance counts substitutions, missing glyphs and spurious blocks.
func screenTextDistance(want, got []rune) int {
	previous := make([]int, len(got)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, a := range want {
		current := make([]int, len(got)+1)
		current[0] = i + 1
		for j, b := range got {
			change := 0
			if a != b {
				change = 1
			}
			current[j+1] = min(previous[j+1]+1, current[j]+1, previous[j]+change)
		}
		previous = current
	}
	return previous[len(got)]
}
