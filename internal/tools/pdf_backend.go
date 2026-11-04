package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/form"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// CPUPDFBackend implements PDFBackend using pure-Go PDF parsing and rewriting.
// Its legacy methods create new files only. PDFOpsTool handles overwrite policy.
type CPUPDFBackend struct{}

var pdfConfigOnce sync.Once

func pdfConfig(password string) *model.Configuration {
	pdfConfigOnce.Do(api.DisableConfigDir)
	c := model.NewDefaultConfiguration()
	c.UserPW, c.OwnerPW = password, password
	c.ValidationMode = model.ValidationRelaxed
	c.Offline = true
	return c
}

// PDFRequest is the additive advanced backend contract. Pages are physical,
// 1-based indexes; Ranges are inclusive ranges such as "1-3".
type PDFRequest struct {
	Action     string         `json:"action"`
	Inputs     []string       `json:"inputs"`
	Output     string         `json:"output,omitempty"`
	OutputDir  string         `json:"output_dir,omitempty"`
	Pages      []int          `json:"pages,omitempty"`
	Ranges     []string       `json:"ranges,omitempty"`
	Angle      int            `json:"angle,omitempty"`
	Notes      []string       `json:"notes,omitempty"`
	Text       string         `json:"text,omitempty"`
	Password   string         `json:"password,omitempty"`
	Overwrite  bool           `json:"overwrite,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
	FieldNames []string       `json:"field_names,omitempty"`
	Page       int            `json:"page,omitempty"`
	DPI        int            `json:"dpi,omitempty"`
}

// PDFOutput describes a successfully written and reopened output.
type PDFOutput struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	PageCount int    `json:"page_count"`
}

// PDFOperationsBackend is optional; existing PDFBackend implementations remain valid.
type PDFOperationsBackend interface {
	ExecutePDF(context.Context, PDFRequest) (any, error)
}

// PDFInfo includes size, encryption, metadata and structured form fields.
type PDFInfo struct {
	Path       string         `json:"path"`
	Size       int64          `json:"size"`
	PageCount  int            `json:"page_count"`
	Encrypted  bool           `json:"encrypted"`
	Metadata   map[string]any `json:"metadata"`
	FormFields []form.Field   `json:"form_fields"`
}

func readPDFInfo(path, password string) (PDFInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return PDFInfo{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return PDFInfo{}, err
	}
	if st.Size() > 128<<20 {
		return PDFInfo{}, errors.New("PDF exceeds 128 MiB limit")
	}
	info, err := api.PDFInfo(f, path, nil, false, pdfConfig(password))
	if err != nil {
		return PDFInfo{}, err
	}
	if info.PageCount > 2000 {
		return PDFInfo{}, errors.New("PDF exceeds 2000 page limit")
	}
	out := PDFInfo{Path: path, Size: st.Size(), PageCount: info.PageCount, Encrypted: info.Encrypted,
		Metadata: map[string]any{"title": info.Title, "author": info.Author, "subject": info.Subject, "creator": info.Creator, "producer": info.Producer, "keywords": info.Keywords, "created": info.CreationDate, "modified": info.ModificationDate, "properties": info.Properties}, FormFields: []form.Field{}}
	if info.Form {
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return out, err
		}
		out.FormFields, err = api.FormFields(f, pdfConfig(password))
	}
	return out, err
}

func pdfSelection(a PDFRequest, count int) ([]string, error) {
	var selected []string
	total := len(a.Pages)
	if total > 2000 {
		return nil, errors.New("page selection exceeds 2000 pages")
	}
	for _, p := range a.Pages {
		if p < 1 || p > count {
			return nil, fmt.Errorf("page %d outside 1..%d", p, count)
		}
		selected = append(selected, strconv.Itoa(p))
	}
	for _, r := range a.Ranges {
		parts := strings.Split(r, "-")
		if len(parts) > 2 {
			return nil, errors.New("invalid page range")
		}
		from, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, errors.New("invalid page range")
		}
		to := from
		if len(parts) == 2 {
			to, err = strconv.Atoi(parts[1])
			if err != nil {
				return nil, errors.New("invalid page range")
			}
		}
		if from < 1 || to < from || to > count {
			return nil, errors.New("page range outside document")
		}
		total += to - from + 1
		if total > 2000 {
			return nil, errors.New("page selection exceeds 2000 pages")
		}
		selected = append(selected, r)
	}
	return selected, nil
}

// pdfPublish uses a sibling temporary file and a no-clobber link unless explicit
// replacement was requested. A failed operation never truncates the destination.
func pdfPublish(ctx context.Context, output string, overwrite bool, write func(*os.File) error) error {
	if output == "" {
		return errors.New("output is required")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(output), ".pdf-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = write(f); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if overwrite {
		return os.Rename(f.Name(), output)
	}
	return os.Link(f.Name(), output)
}

func pdfOutput(path, password string) (PDFOutput, error) {
	info, err := readPDFInfo(path, password)
	if err != nil {
		return PDFOutput{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return PDFOutput{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return PDFOutput{}, err
	}
	return PDFOutput{Path: path, SHA256: hex.EncodeToString(h.Sum(nil)), PageCount: info.PageCount}, nil
}

// ExecutePDF executes a bounded structural operation. Cancellation is checked
// before parsing and before publishing; a pdfcpu call finishes before returning.
func (b *CPUPDFBackend) ExecutePDF(ctx context.Context, a PDFRequest) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(a.Inputs) == 0 || len(a.Inputs) > 100 {
		return nil, errors.New("inputs must contain 1..100 files")
	}
	infos := make([]PDFInfo, len(a.Inputs))
	for i, p := range a.Inputs {
		info, err := readPDFInfo(p, a.Password)
		if err != nil {
			return nil, err
		}
		infos[i] = info
	}
	input := a.Inputs[0]
	info := infos[0]
	selected, err := pdfSelection(a, info.PageCount)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(input)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	conf := pdfConfig(a.Password)
	switch a.Action {
	case "info":
		return info, nil
	case "validate":
		err = api.Validate(f, conf)
		return map[string]any{"valid": err == nil, "page_count": info.PageCount}, err
	case "form_fields":
		return info.FormFields, nil
	case "attachments":
		items, err := api.Attachments(f, conf)
		if err != nil {
			return nil, err
		}
		listed := make([]map[string]any, 0, len(items))
		for _, item := range items {
			listed = append(listed, map[string]any{"id": item.ID, "name": item.FileName, "description": item.Desc, "modified": item.ModTime})
		}
		return listed, nil
	case "bookmarks":
		return api.Bookmarks(f, conf)
	case "split":
		if a.OutputDir == "" {
			return nil, errors.New("output_dir is required")
		}
		groups := selected
		if len(groups) == 0 {
			for p := 1; p <= info.PageCount; p++ {
				groups = append(groups, strconv.Itoa(p))
			}
		}
		// Preflight every destination before writing any split output.
		paths := make([]string, len(groups))
		base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		for i := range groups {
			paths[i] = filepath.Join(a.OutputDir, fmt.Sprintf("%s-p%04d.pdf", base, i+1))
			if !a.Overwrite {
				if _, e := os.Lstat(paths[i]); !os.IsNotExist(e) {
					return nil, errors.New("split output exists or cannot be inspected")
				}
			}
		}
		outputs := make([]PDFOutput, 0, len(groups))
		for i, g := range groups {
			req := a
			req.Action = "extract_pages"
			req.Output = paths[i]
			req.OutputDir = ""
			req.Pages = nil
			req.Ranges = []string{g}
			r, e := b.ExecutePDF(ctx, req)
			if e != nil {
				return nil, e
			}
			outputs = append(outputs, r.(PDFOutput))
		}
		return map[string]any{"outputs": outputs}, nil
	}
	if a.Output == "" {
		return nil, errors.New("output is required")
	}
	if !a.Overwrite {
		if _, e := os.Lstat(a.Output); !os.IsNotExist(e) {
			return nil, errors.New("output exists or cannot be inspected; use overwrite:true to replace")
		}
	}
	// Validate choices before allocating the output file.
	switch a.Action {
	case "extract_pages", "delete_pages", "reorder":
		if len(selected) == 0 {
			return nil, errors.New("pages or ranges are required")
		}
	case "rotate":
		if a.Angle%90 != 0 {
			return nil, errors.New("angle must be a multiple of 90")
		}
	case "encrypt", "decrypt":
		if a.Password == "" {
			return nil, errors.New("password is required")
		}
	case "redact":
		return nil, errors.New("secure redaction is not supported")
	case "flatten", "form_flatten":
		return nil, errors.New("flatten is not supported by this backend; form_lock preserves fields and their appearances")
	}
	err = pdfPublish(ctx, a.Output, a.Overwrite, func(w *os.File) (err error) {
		// Windows cannot replace a destination while its input reader is open.
		defer func() {
			if closeErr := closePDFInputForPublish(f); err == nil {
				err = closeErr
			}
		}()
		switch a.Action {
		case "merge":
			if len(a.Inputs) < 2 {
				return errors.New("merge requires at least two inputs")
			}
			total := 0
			for _, i := range infos {
				total += i.PageCount
			}
			if total > 2000 {
				return errors.New("merged output exceeds 2000 pages")
			}
			readers := make([]io.ReadSeeker, 0, len(a.Inputs))
			for _, p := range a.Inputs {
				r, e := os.Open(p)
				if e != nil {
					return e
				}
				defer r.Close()
				readers = append(readers, r)
			}
			return api.MergeRaw(readers, w, false, conf)
		case "extract_pages", "reorder":
			return api.Collect(f, w, selected, conf)
		case "delete_pages":
			return api.RemovePages(f, w, selected, conf)
		case "rotate":
			return api.Rotate(f, w, a.Angle, selected, conf)
		case "stamp", "watermark", "annotate", "page_numbers":
			text := a.Text
			if a.Action == "annotate" && text == "" {
				text = strings.Join(a.Notes, "\n")
			}
			desc := "fontname:Helvetica, points:18, rotation:0, position:bc, offset:0 12"
			if a.Action == "page_numbers" {
				text = "%p / %P"
			}
			if text == "" {
				return errors.New("text is required")
			}
			wm, e := api.TextWatermark(text, desc, a.Action != "watermark", false, types.POINTS)
			if e != nil {
				return e
			}
			return api.AddWatermarks(f, w, selected, wm, conf)
		case "encrypt":
			conf.EncryptUsingAES = true
			conf.EncryptKeyLength = 256
			return api.Encrypt(f, w, conf)
		case "decrypt":
			return api.Decrypt(f, w, conf)
		case "optimize":
			return api.Optimize(f, w, conf)
		case "form_lock":
			return lockPDFForm(f, w, info.FormFields, a.FieldNames, conf)
		case "form_fill", "fill_form":
			return fillPDFForm(f, w, info.FormFields, a.Fields, conf)
		default:
			return fmt.Errorf("unknown PDF action %q", a.Action)
		}
	})
	if err != nil {
		return nil, err
	}
	return pdfOutput(a.Output, a.Password)
}

func fillPDFForm(r io.ReadSeeker, w io.Writer, fields []form.Field, values map[string]any, c *model.Configuration) error {
	if len(values) == 0 {
		return errors.New("fields is required")
	}

	ctx, err := api.ReadValidateAndOptimize(r, c)
	if err != nil {
		return err
	}
	if ctx.Form != nil && ctx.Form["XFA"] != nil {
		return errors.New("XFA forms are not supported")
	}
	var buf bytes.Buffer
	if _, err = form.ExportFormJSON(ctx.XRefTable, "", &buf); err != nil {
		return err
	}
	var original form.FormGroup
	if err = json.Unmarshal(buf.Bytes(), &original); err != nil {
		return err
	}
	if len(original.Forms) != 1 {
		return errors.New("expected one AcroForm")
	}
	choices := map[string][]string{}
	for _, f := range original.Forms[0].ComboBoxes {
		choices[f.ID] = f.Options
	}
	for _, f := range original.Forms[0].ListBoxes {
		choices[f.ID] = f.Options
	}
	for _, f := range original.Forms[0].RadioButtonGroups {
		choices[f.ID] = f.Options
	}
	if _, err = r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	result := form.Form{}
	for name, value := range values {
		var field *form.Field
		for i := range fields {
			if fields[i].Name == name || fields[i].ID == name {
				field = &fields[i]
				break
			}
		}
		if field == nil {
			return errors.New("unknown form field")
		}
		if field.Locked {
			return errors.New("form field is locked")
		}
		switch field.Typ {
		case form.FTText:
			v, ok := value.(string)
			if !ok {
				return errors.New("text field requires a string")
			}
			result.TextFields = append(result.TextFields, &form.TextField{ID: field.ID, Value: v})
		case form.FTCheckBox:
			v, ok := value.(bool)
			if !ok {
				return errors.New("checkbox requires a boolean")
			}
			result.CheckBoxes = append(result.CheckBoxes, &form.CheckBox{ID: field.ID, Value: v})
		case form.FTComboBox:
			v, ok := value.(string)
			if !ok {
				return errors.New("choice requires a string")
			}
			if err := pdfChoiceValues(choices[field.ID], []string{v}); err != nil {
				return err
			}
			result.ComboBoxes = append(result.ComboBoxes, &form.ComboBox{ID: field.ID, Value: v, Options: choices[field.ID]})
		case form.FTListBox:
			var vv []string
			switch v := value.(type) {
			case string:
				vv = []string{v}
			case []any:
				for _, x := range v {
					s, ok := x.(string)
					if !ok {
						return errors.New("list choice requires strings")
					}
					vv = append(vv, s)
				}
			default:
				return errors.New("list choice requires strings")
			}
			if err := pdfChoiceValues(choices[field.ID], vv); err != nil {
				return err
			}
			result.ListBoxes = append(result.ListBoxes, &form.ListBox{ID: field.ID, Values: vv, Options: choices[field.ID]})
		case form.FTRadioButtonGroup:
			v, ok := value.(string)
			if !ok {
				return errors.New("radio choice requires a string")
			}
			if err := pdfChoiceValues(choices[field.ID], []string{v}); err != nil {
				return err
			}
			result.RadioButtonGroups = append(result.RadioButtonGroups, &form.RadioButtonGroup{ID: field.ID, Value: v, Options: choices[field.ID]})
		default:
			return errors.New("form field type is not supported")
		}
	}
	raw, err := json.Marshal(form.FormGroup{Forms: []form.Form{result}})
	if err != nil {
		return err
	}
	return api.FillForm(r, bytes.NewReader(raw), w, c)
}

func (b *CPUPDFBackend) Merge(inputs []string, output string) error {
	_, e := b.ExecutePDF(context.Background(), PDFRequest{Action: "merge", Inputs: inputs, Output: output})
	return e
}
func (b *CPUPDFBackend) Split(input, dir string) ([]string, error) {
	info, e := readPDFInfo(input, "")
	if e != nil {
		return nil, e
	}
	out := make([]string, 0, info.PageCount)
	for i := 1; i <= info.PageCount; i++ {
		p := filepath.Join(dir, fmt.Sprintf("%s-p%04d.pdf", strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)), i))
		if e = b.ExtractPages(input, []int{i}, p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
func (b *CPUPDFBackend) ExtractPages(input string, pages []int, output string) error {
	_, e := b.ExecutePDF(context.Background(), PDFRequest{Action: "extract_pages", Inputs: []string{input}, Output: output, Pages: pages})
	return e
}
func (b *CPUPDFBackend) Rotate(input string, angle int, output string) error {
	_, e := b.ExecutePDF(context.Background(), PDFRequest{Action: "rotate", Inputs: []string{input}, Output: output, Angle: angle})
	return e
}
func (b *CPUPDFBackend) Annotate(input string, notes []string, output string) error {
	_, e := b.ExecutePDF(context.Background(), PDFRequest{Action: "annotate", Inputs: []string{input}, Output: output, Notes: notes})
	return e
}

// lockPDFForm sets the AcroForm read-only bit while retaining the existing
// appearance streams. It does not regenerate fonts or pretend to flatten.
func lockPDFForm(r io.ReadSeeker, w io.Writer, fields []form.Field, names []string, conf *model.Configuration) error {
	if len(fields) == 0 {
		return errors.New("no AcroForm fields available")
	}
	selected := map[string]bool{}
	for _, name := range names {
		found := false
		for _, field := range fields {
			if name == field.ID || name == field.Name {
				selected[field.ID] = true
				found = true
			}
		}
		if !found {
			return errors.New("unknown form field")
		}
	}
	ctx, err := api.ReadValidateAndOptimize(r, conf)
	if err != nil {
		return err
	}
	if ctx.Form != nil && ctx.Form["XFA"] != nil {
		return errors.New("XFA forms are not supported")
	}
	for _, field := range fields {
		if len(names) > 0 && !selected[field.ID] {
			continue
		}
		parts := strings.Split(field.ID, ".")
		number, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return errors.New("invalid AcroForm field ID")
		}
		entry := ctx.Table[number]
		if entry == nil || entry.Generation == nil {
			return errors.New("missing AcroForm field object")
		}
		d, err := ctx.DereferenceDict(*types.NewIndirectRef(number, *entry.Generation))
		if err != nil {
			return err
		}
		flags := 0
		if n := d.IntEntry("Ff"); n != nil {
			flags = *n
		}
		d["Ff"] = types.Integer(flags | 1)
	}
	return api.Write(ctx, w, conf)
}

func pdfChoiceValues(options, values []string) error {
	for _, value := range values {
		if value == "" {
			continue
		}
		found := false
		for _, option := range options {
			if option == value {
				found = true
				break
			}
		}
		if !found {
			return errors.New("choice value is not an available form option")
		}
	}
	return nil
}
