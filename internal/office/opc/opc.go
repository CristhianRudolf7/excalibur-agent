// Package opc reads and writes bounded OOXML packages without altering opaque parts.
package opc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const MaxBytes = 64 << 20
const MaxParts = 4096
const RelNS = "http://schemas.openxmlformats.org/package/2006/relationships"
const OfficeRel = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
const ContentNS = "http://schemas.openxmlformats.org/package/2006/content-types"

// Package retains each part's exact uncompressed bytes, including unknown parts.
type Package struct{ Parts map[string][]byte }

func New() *Package        { return &Package{Parts: map[string][]byte{}} }
func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func Open(b []byte) (*Package, error) {
	if len(b) > MaxBytes {
		return nil, fmt.Errorf("package exceeds %d bytes", MaxBytes)
	}
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		return nil, e
	}
	if len(z.File) > MaxParts {
		return nil, fmt.Errorf("too many parts")
	}
	p := New()
	total := int64(0)
	for _, f := range z.File {
		n := f.Name
		if strings.HasSuffix(n, "/") {
			continue
		}
		if !validName(n) || f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe part %q", n)
		}
		if _, ok := p.Parts[n]; ok {
			return nil, fmt.Errorf("duplicate part %q", n)
		}
		if f.UncompressedSize64 > MaxBytes {
			return nil, fmt.Errorf("part too large")
		}
		r, e := f.Open()
		if e != nil {
			return nil, e
		}
		b, e := io.ReadAll(io.LimitReader(r, MaxBytes-total+1))
		_ = r.Close()
		if e != nil {
			return nil, e
		}
		total += int64(len(b))
		if total > MaxBytes {
			return nil, fmt.Errorf("expanded package too large")
		}
		p.Parts[n] = b
	}
	return p, nil
}
func validName(n string) bool {
	return n != "" && !strings.Contains(n, "\\") && !strings.Contains(n, ":") && !strings.HasPrefix(n, "/") && path.Clean(n) == n && n != ".." && !strings.HasPrefix(n, "../")
}

// CheckLimits bounds package growth between operations without serializing ZIP data.
func (p *Package) CheckLimits() error {
	if len(p.Parts) > MaxParts {
		return fmt.Errorf("too many package parts")
	}
	total := 0
	for _, data := range p.Parts {
		total += len(data)
		if total > MaxBytes {
			return fmt.Errorf("expanded package too large")
		}
	}
	return nil
}

func (p *Package) Bytes() ([]byte, error) {
	if err := p.CheckLimits(); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	names := make([]string, 0, len(p.Parts))
	for n := range p.Parts {
		names = append(names, n)
	}
	sort.Strings(names)
	total := 0
	for _, n := range names {
		if !validName(n) {
			return nil, fmt.Errorf("unsafe part %q", n)
		}
		total += len(p.Parts[n])
		if total > MaxBytes || len(names) > MaxParts {
			return nil, fmt.Errorf("package limits exceeded")
		}
		w, e := z.Create(n)
		if e != nil {
			return nil, e
		}
		if _, e = w.Write(p.Parts[n]); e != nil {
			return nil, e
		}
	}
	if e := z.Close(); e != nil {
		return nil, e
	}
	if b.Len() > MaxBytes {
		return nil, fmt.Errorf("package too large")
	}
	return b.Bytes(), nil
}

// Relationship is an OPC relationship; external links are retained but never fetched.
type Relationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
	Mode   string `xml:"TargetMode,attr,omitempty"`
}

func (p *Package) Relationships(part string) ([]Relationship, error) {
	var v struct {
		XMLName xml.Name
		Items   []Relationship `xml:"Relationship"`
	}
	e := xml.Unmarshal(p.Parts[part], &v)
	if e == nil && (v.XMLName.Local != "Relationships" || v.XMLName.Space != RelNS) {
		e = fmt.Errorf("invalid relationships root")
	}
	return v.Items, e
}
func (p *Package) SetRelationships(part string, rels []Relationship) {
	s := `<Relationships xmlns="` + RelNS + `">`
	for _, r := range rels {
		s += `<Relationship Id="` + Escape(r.ID) + `" Type="` + Escape(r.Type) + `" Target="` + Escape(r.Target) + `"`
		if r.Mode != "" {
			s += ` TargetMode="` + Escape(r.Mode) + `"`
		}
		s += `/>`
	}
	p.Parts[part] = []byte(s + `</Relationships>`)
}
func RelsPath(part string) string { return path.Join(path.Dir(part), "_rels", path.Base(part)+".rels") }
func Resolve(rels, target string) string {
	if decoded, e := url.PathUnescape(target); e == nil {
		target = decoded
	}
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}
	base := path.Dir(path.Dir(rels))
	return path.Clean(path.Join(base, target))
}

// Types creates the content type table. Overrides are absolute package part names.
func (p *Package) Types(overrides map[string]string) {
	s := `<Types xmlns="` + ContentNS + `"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Default Extension="jpg" ContentType="image/jpeg"/><Default Extension="jpeg" ContentType="image/jpeg"/>`
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s += `<Override PartName="/` + Escape(k) + `" ContentType="` + Escape(overrides[k]) + `"/>`
	}
	p.Parts["[Content_Types].xml"] = []byte(s + `</Types>`)
}

// Validate checks XML syntax, content coverage and all internal relationship targets.
func (p *Package) Validate() []string {
	issues := []string{}
	var ct struct {
		XMLName  xml.Name
		Defaults []struct {
			Ext  string `xml:"Extension,attr"`
			Type string `xml:"ContentType,attr"`
		} `xml:"Default"`
		Overrides []struct {
			Name string `xml:"PartName,attr"`
			Type string `xml:"ContentType,attr"`
		} `xml:"Override"`
	}
	if e := xml.Unmarshal(p.Parts["[Content_Types].xml"], &ct); e != nil {
		issues = append(issues, "invalid content types: "+e.Error())
	}
	if ct.XMLName.Local != "Types" || ct.XMLName.Space != ContentNS {
		issues = append(issues, "invalid content types root")
	}
	if _, ok := p.Parts["_rels/.rels"]; !ok {
		issues = append(issues, "missing package relationships")
	}
	covered := map[string]bool{}
	exts := map[string]bool{}
	for _, d := range ct.Defaults {
		exts[d.Ext] = d.Type != ""
	}
	for _, o := range ct.Overrides {
		n := strings.TrimPrefix(o.Name, "/")
		if decoded, e := url.PathUnescape(n); e == nil {
			n = decoded
		}
		covered[n] = o.Type != ""
		if _, ok := p.Parts[n]; !ok {
			issues = append(issues, "missing content type target: "+n)
		}
	}
	for n, b := range p.Parts {
		if n != "[Content_Types].xml" && !covered[n] && !exts[strings.TrimPrefix(path.Ext(n), ".")] {
			issues = append(issues, "missing content type: "+n)
		}
		if strings.HasSuffix(n, ".xml") || strings.HasSuffix(n, ".rels") {
			d := xml.NewDecoder(bytes.NewReader(b))
			roots, depth := 0, 0
			tokens := 0
		scanXML:
			for {
				t, e := d.Token()
				if e == io.EOF {
					break
				}
				if e != nil {
					issues = append(issues, n+": "+e.Error())
					break
				}
				switch token := t.(type) {
				case xml.Directive:
					if strings.Contains(strings.ToUpper(string(token)), "DOCTYPE") {
						issues = append(issues, n+": DTD declarations are unsupported")
					}
				case xml.CharData:
					if depth == 0 && strings.TrimSpace(string(token)) != "" {
						issues = append(issues, n+": text outside XML root")
					}
				case xml.StartElement:
					if depth == 0 {
						roots++
					}
					depth++
					tokens++
					if depth > 256 || tokens > 1000000 {
						issues = append(issues, n+": XML complexity limit exceeded")
						break scanXML
					}
				case xml.EndElement:
					depth--
				}
			}
			if roots != 1 || depth != 0 {
				issues = append(issues, n+": invalid XML document")
			}
		}
		if strings.HasSuffix(n, ".rels") {
			rels, e := p.Relationships(n)
			if e != nil {
				issues = append(issues, n+": "+e.Error())
				continue
			}
			ids := map[string]bool{}
			for _, r := range rels {
				if ids[r.ID] || r.ID == "" {
					issues = append(issues, n+": duplicate or empty relationship ID")
				}
				ids[r.ID] = true
				if r.Mode == "External" {
					continue
				}
				target := Resolve(n, strings.Split(r.Target, "#")[0])
				if _, ok := p.Parts[target]; !ok {
					issues = append(issues, n+": missing relationship target "+target)
				}
			}
		}
	}
	sort.Strings(issues)
	return issues
}

// HashMismatch reports the current file hash without modifying the destination.
type HashMismatch struct{ Current string }

func (e *HashMismatch) Error() string { return "expected_sha256 mismatch; current_sha256=" + e.Current }

var writeMu sync.Mutex

// WriteAtomic validates a serialized package before committing. Empty expected means create.
// Creation uses a hard link to publish without clobbering a concurrently created file.
func WriteAtomic(filename string, p *Package, expected string, overwrite bool) (string, []string, error) {
	b, e := p.Bytes()
	if e != nil {
		return "", nil, e
	}
	q, e := Open(b)
	if e != nil {
		return "", nil, e
	}
	issues := q.Validate()
	if len(issues) > 0 {
		return "", issues, fmt.Errorf("package validation failed: %s", strings.Join(issues, "; "))
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	mode := os.FileMode(0600)
	check := func() error {
		old, e := os.ReadFile(filename)
		if expected != "" {
			if e != nil {
				return e
			}
			h := Hash(old)
			if h != expected {
				return &HashMismatch{h}
			}
		} else if !overwrite {
			if e == nil {
				return fmt.Errorf("destination exists")
			}
			if !os.IsNotExist(e) {
				return e
			}
		}
		if st, e := os.Stat(filename); e == nil {
			mode = st.Mode().Perm()
		}
		return nil
	}
	if e = check(); e != nil {
		return "", issues, e
	}
	f, e := os.CreateTemp(filepath.Dir(filename), ".office-*")
	if e != nil {
		return "", issues, e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", issues, e
	}
	if e = check(); e != nil {
		return "", issues, e
	}
	if expected == "" && !overwrite {
		e = os.Link(f.Name(), filename)
	} else {
		e = os.Rename(f.Name(), filename)
	}
	if e != nil {
		return "", issues, e
	}
	return Hash(b), issues, nil
}
