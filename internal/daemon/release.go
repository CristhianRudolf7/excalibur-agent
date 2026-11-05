package daemon

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ReleasePublicKey is the sole trust root for public binary updates. Set this
// to the public hex output of scripts/release/genkey before the first release.
// An empty key deliberately fails closed; never ship a temporary signing key.
const ReleasePublicKey = ""

const ReleaseBaseURL = "https://github.com/CristhianRudolf7/excalibur-agent/releases/latest/download"

// Release holds the signed checksum document and its exact downloaded artifacts.
// Verification is repeated immediately before the updater stages any bytes.
type Release struct {
	Checksums, Signature, Archive, VersionFile []byte
	OS, Arch                                   string
}

func (r Release) archiveName() string {
	ext := ".tar.gz"
	if r.OS == "windows" {
		ext = ".zip"
	}
	return "excalibur_" + r.OS + "_" + r.Arch + ext
}

func checksumMatches(checksums []byte, name string, data []byte) bool {
	sum := sha256.Sum256(data)
	expected := hex.EncodeToString(sum[:])
	count := 0
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			if fields[0] != expected {
				return false
			}
			count++
		}
	}
	return count == 1
}

// Verify authenticates checksums before trusting the archive or version.
func (r Release) Verify(pub ed25519.PublicKey) (Manifest, []byte, error) {
	var m Manifest
	signature, err := hex.DecodeString(strings.TrimSpace(string(r.Signature)))
	if err != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, r.Checksums, signature) {
		return m, nil, fmt.Errorf("release: checksum signature rejected")
	}
	if !checksumMatches(r.Checksums, r.archiveName(), r.Archive) || !checksumMatches(r.Checksums, "version.txt", r.VersionFile) {
		return m, nil, fmt.Errorf("release: checksum mismatch")
	}
	version := strings.TrimSpace(string(r.VersionFile))
	if version == "" || strings.ContainsAny(version, "\r\n/\\ ") {
		return m, nil, fmt.Errorf("release: invalid version")
	}
	payload, err := unpackRelease(r.Archive, r.OS)
	if err != nil {
		return m, nil, err
	}
	sum := sha256.Sum256(payload)
	m = Manifest{Channel: ChannelStable, Version: version, SHA256: hex.EncodeToString(sum[:])}
	return m, payload, nil
}

const maxReleaseBytes = 512 << 20

func unpackRelease(raw []byte, goos string) ([]byte, error) {
	if goos == "windows" {
		reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if file.Name == "excalibur.exe" && file.Mode().IsRegular() {
				stream, err := file.Open()
				if err != nil {
					return nil, err
				}
				defer stream.Close()
				return readReleaseBytes(stream)
			}
		}
	} else {
		stream, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		reader := tar.NewReader(stream)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if header.Name == "excalibur" && header.Typeflag == tar.TypeReg {
				return readReleaseBytes(reader)
			}
		}
	}
	return nil, fmt.Errorf("release: archive contains no executable")
}
func readReleaseBytes(reader io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, maxReleaseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxReleaseBytes {
		return nil, fmt.Errorf("release: artifact exceeds 512 MiB")
	}
	return raw, nil
}

// FetchRelease downloads without credentials. A custom base supports offline
// smoke tests; authenticity still depends on the compiled public key.
func FetchRelease(ctx context.Context, base, goos, arch string) (Release, error) {
	r := Release{OS: goos, Arch: arch}
	client := &http.Client{Timeout: 3 * time.Minute}
	if base == "" {
		base = ReleaseBaseURL
	}
	for _, item := range []struct {
		name   string
		target *[]byte
	}{
		{"checksums.txt", &r.Checksums}, {"checksums.txt.sig", &r.Signature}, {"version.txt", &r.VersionFile}, {r.archiveName(), &r.Archive},
	} {
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/"+item.name, nil)
		if err != nil {
			return r, err
		}
		response, err := client.Do(req)
		if err != nil {
			return r, fmt.Errorf("release: download %s failed", item.name)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return r, fmt.Errorf("release: download %s: HTTP %d", item.name, response.StatusCode)
		}
		raw, err := readReleaseBytes(response.Body)
		response.Body.Close()
		if err != nil {
			return r, err
		}
		*item.target = raw
	}
	return r, nil
}

// ApplyRelease uses the same stage/swap/rollback state machine as local signed
// manifests, after checking the release checksum signature and archive hash.
func (u *Updater) ApplyRelease(ctx context.Context, release Release) (UpdateReport, error) {
	m, payload, err := release.Verify(u.PubKey)
	if err != nil {
		return UpdateReport{}, err
	}
	return u.applyVerified(ctx, m, payload)
}
