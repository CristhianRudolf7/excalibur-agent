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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func releaseFixture(t *testing.T, goos string) (Release, ed25519.PublicKey) {
	t.Helper()
	pub, priv := testKeys(t)
	r := Release{OS: goos, Arch: "amd64", VersionFile: []byte("v1.2.3\n")}
	var buf bytes.Buffer
	if goos == "windows" {
		writer := zip.NewWriter(&buf)
		file, err := writer.Create("excalibur.exe")
		if err != nil {
			t.Fatal(err)
		}
		file.Write([]byte("new binary"))
		writer.Close()
	} else {
		gz := gzip.NewWriter(&buf)
		writer := tar.NewWriter(gz)
		writer.WriteHeader(&tar.Header{Name: "excalibur", Mode: 0755, Size: 10, Typeflag: tar.TypeReg})
		writer.Write([]byte("new binary"))
		writer.Close()
		gz.Close()
	}
	r.Archive = buf.Bytes()
	archiveSum, versionSum := sha256.Sum256(r.Archive), sha256.Sum256(r.VersionFile)
	r.Checksums = []byte(fmt.Sprintf("%x  %s\n%x  version.txt\n", archiveSum, r.archiveName(), versionSum))
	r.Signature = []byte(hex.EncodeToString(ed25519.Sign(priv, r.Checksums)))
	return r, pub
}

func TestSignedPublicRelease(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			r, pub := releaseFixture(t, goos)
			files := map[string][]byte{"checksums.txt": r.Checksums, "checksums.txt.sig": r.Signature, "version.txt": r.VersionFile, r.archiveName(): r.Archive}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				raw, ok := files[req.URL.Path[1:]]
				if !ok {
					http.NotFound(w, req)
					return
				}
				w.Write(raw)
			}))
			defer server.Close()
			fetched, err := FetchRelease(context.Background(), server.URL, goos, "amd64")
			if err != nil {
				t.Fatal(err)
			}
			m, payload, err := fetched.Verify(pub)
			if err != nil || m.Version != "v1.2.3" || string(payload) != "new binary" {
				t.Fatal(m, err)
			}
			images := &fakeImages{version: "old"}
			u := Updater{PubKey: pub, Images: images, Health: &fakeHealth{err: errors.New("boot failed")}}
			report, err := u.ApplyRelease(context.Background(), fetched)
			if err == nil || !images.rolledBk || report.RolledBack == "" {
				t.Fatal("release bypassed health rollback", report, err)
			}
			for _, field := range []string{"checksums", "signature", "archive", "version"} {
				t.Run(field, func(t *testing.T) {
					bad := fetched
					switch field {
					case "checksums":
						bad.Checksums = append([]byte("bad"), bad.Checksums...)
					case "signature":
						bad.Signature = []byte("00")
					case "archive":
						bad.Archive = []byte("bad")
					case "version":
						bad.VersionFile = []byte("v9\n")
					}
					images := &fakeImages{}
					u := Updater{PubKey: pub, Images: images}
					if _, err := u.ApplyRelease(context.Background(), bad); err == nil || images.staged != nil {
						t.Fatal("tampering staged a binary")
					}
				})
			}
		})
	}
}
