package tools

import (
	"context"
	"encoding/xml"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func finishDiagramOutput(data, format string, max int64) (string, error) {
	if int64(len(data)) > max {
		return "", fmt.Errorf("diagram output exceeds %d bytes", max)
	}
	var err error
	switch format {
	case "svg":
		data, err = sanitizeDiagramSVG(data)
	case "png":
		if !strings.HasPrefix(data, "\x89PNG\r\n\x1a\n") {
			err = fmt.Errorf("renderer did not produce PNG data")
		}
	case "pdf":
		if !strings.HasPrefix(data, "%PDF-") {
			err = fmt.Errorf("renderer did not produce PDF data")
		}
	}
	if err != nil {
		return "", err
	}
	if int64(len(data)) > max {
		return "", fmt.Errorf("sanitized diagram output exceeds %d bytes", max)
	}
	return data, nil
}

func diagramFormatUnavailable(err error) bool {
	s := strings.ToLower(err.Error())
	return (strings.Contains(s, "format") && (strings.Contains(s, "not recognized") || strings.Contains(s, "not supported") || strings.Contains(s, "unsupported"))) ||
		strings.Contains(s, "classnotfoundexception") || strings.Contains(s, "org.apache.batik") || strings.Contains(s, "org.apache.fop") ||
		strings.Contains(s, "playwright install") || strings.Contains(s, "browser not found") || strings.Contains(s, "could not find chrome")
}

func (b *SVGDiagramBackend) convertSVG(ctx context.Context, svg, format string) (string, string, error) {
	dir, err := os.MkdirTemp(b.TempDir, "excalibur-diagram-convert-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)
	input, output := filepath.Join(dir, "input.svg"), filepath.Join(dir, "output."+format)
	if err := os.WriteFile(input, []byte(svg), 0o600); err != nil {
		return "", "", err
	}
	var bin string
	var args []string
	if path, err := b.lookup("rsvg-convert"); err == nil {
		bin, args = path, []string{"--format=" + format, "--output=" + output, input}
	} else {
		for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
			if path, err := b.lookup(name); err == nil {
				bin = path
				break
			}
		}
		if bin == "" {
			return "", "", fmt.Errorf("SVG to %s needs rsvg-convert or an installed headless Chromium; install librsvg2-bin with apt-get, librsvg2-tools with dnf, or librsvg with brew; alternatively use a dialect with native %s output such as graphviz", format, format)
		}
		args = []string{"--headless", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--disable-extensions", "--no-first-run", "--hide-scrollbars", "--user-data-dir=" + filepath.Join(dir, "browser"), "--force-device-scale-factor=1", "--timeout=15000"}
		if format == "png" {
			w, h := diagramSVGSize(svg)
			args = append(args, fmt.Sprintf("--window-size=%d,%d", w, h), "--default-background-color=00000000", "--screenshot="+output)
		} else {
			args = append(args, "--no-pdf-header-footer", "--print-to-pdf="+output)
		}
		args = append(args, (&url.URL{Scheme: "file", Path: input}).String())
	}
	data, isolation, err := b.run(ctx, dir, bin, args, "", output, false)
	if err != nil {
		return "", "", fmt.Errorf("%s conversion: %w", filepath.Base(bin), err)
	}
	data, err = finishDiagramOutput(data, format, b.limit())
	return data, filepath.Base(bin) + " (" + isolation + ")", err
}

func diagramSVGSize(svg string) (int, int) {
	var root struct {
		Width   string `xml:"width,attr"`
		Height  string `xml:"height,attr"`
		ViewBox string `xml:"viewBox,attr"`
	}
	if err := xml.Unmarshal([]byte(svg), &root); err != nil {
		return 1280, 720
	}
	dim := func(s string) int {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 1 || v > 8192 {
			return 0
		}
		return int(v + 0.5)
	}
	w, h := dim(root.Width), dim(root.Height)
	if parts := strings.Fields(root.ViewBox); len(parts) == 4 {
		if w == 0 {
			w = dim(parts[2])
		}
		if h == 0 {
			h = dim(parts[3])
		}
	}
	if w == 0 {
		w = 1280
	}
	if h == 0 {
		h = 720
	}
	return w, h
}
