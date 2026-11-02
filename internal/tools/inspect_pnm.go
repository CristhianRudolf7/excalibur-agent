package tools

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"io"
	"strconv"
)

func pnmSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}

// Each token consumes exactly its final separator, preserving binary raster
// bytes even when their values happen to be whitespace or '#'.
func pnmToken(r *bufio.Reader) (string, error) {
	var token []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && len(token) > 0 {
				return string(token), nil
			}
			return "", err
		}
		if c == '#' {
			for c != '\n' {
				c, err = r.ReadByte()
				if err != nil {
					return "", err
				}
			}
			if len(token) > 0 {
				return string(token), nil
			}
			continue
		}
		if pnmSpace(c) {
			if len(token) == 0 {
				continue
			}
			if c == '\r' {
				if next, _ := r.Peek(1); len(next) > 0 && next[0] == '\n' {
					_, _ = r.ReadByte()
				}
			}
			return string(token), nil
		}
		token = append(token, c)
		if len(token) > 32 {
			return "", fmt.Errorf("PNM token too long")
		}
	}
}
func pnmNumber(r *bufio.Reader) (int, error) {
	s, err := pnmToken(r)
	if err != nil {
		return 0, err
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid PNM number")
		}
	}
	return strconv.Atoi(s)
}
func pnmHeader(r *bufio.Reader) (kind string, w, h, maximum int, err error) {
	kind, err = pnmToken(r)
	if err != nil {
		return
	}
	if kind != "P2" && kind != "P3" && kind != "P5" && kind != "P6" {
		err = fmt.Errorf("unsupported PNM kind")
		return
	}
	w, err = pnmNumber(r)
	if err != nil {
		return
	}
	h, err = pnmNumber(r)
	if err != nil {
		return
	}
	maximum, err = pnmNumber(r)
	if err == nil && (maximum < 1 || maximum > 65535 || !imageDimensionsOK(w, h)) {
		err = fmt.Errorf("PNM dimensions exceed 64 MP or invalid max value")
	}
	return
}
func pnmConfig(r *bufio.Reader) (image.Config, string, error) {
	kind, w, h, _, err := pnmHeader(r)
	format := "ppm"
	if kind == "P2" || kind == "P5" {
		format = "pgm"
	}
	return image.Config{ColorModel: color.NRGBAModel, Width: w, Height: h}, format, err
}
func decodePNM(r *bufio.Reader) (image.Image, error) {
	kind, w, h, maximum, err := pnmHeader(r)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	sample := func() (uint8, error) {
		var v int
		var err error
		if kind == "P2" || kind == "P3" {
			v, err = pnmNumber(r)
		} else {
			var c byte
			c, err = r.ReadByte()
			v = int(c)
			if err == nil && maximum > 255 {
				c, err = r.ReadByte()
				v = v*256 + int(c)
			}
		}
		if err != nil {
			return 0, err
		}
		if v > maximum {
			return 0, fmt.Errorf("PNM sample exceeds max value")
		}
		return uint8((v*255 + maximum/2) / maximum), nil
	}
	for i := 0; i < len(img.Pix); i += 4 {
		rv, err := sample()
		if err != nil {
			return nil, err
		}
		gv, bv := rv, rv
		if kind == "P3" || kind == "P6" {
			gv, err = sample()
			if err != nil {
				return nil, err
			}
			bv, err = sample()
			if err != nil {
				return nil, err
			}
		}
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = rv, gv, bv, 255
	}
	return img, nil
}
