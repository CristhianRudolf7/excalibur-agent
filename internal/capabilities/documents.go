package capabilities

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Documents capability (Section 14.7): read, create, edit and convert
// office documents and formats, plus the user databases that live
// apart from the harness-internal stores.

// ConvertPairs is the manifest pair list of file_convert: the only
// conversions the harness claims. Anything else is an input error;
// universal compatibility is never supposed (Section 14.7).
var ConvertPairs = []string{"md->html", "txt->md", "md->txt", "json->md", "csv->json", "html->md"}

// DocumentsManifest publishes the document and user-database operations.
func DocumentsManifest() Manifest {
	return Manifest{
		ID: "documents", Version: "1",
		Operations: []Operation{
			Op("document_read", "Extract text and structure of any supported format with page, section and paragraph anchors", "read_only", "pure", "immediate", false, "documents", []string{"document", "read", "extract", "anchors"}, ObjSchema([]string{"path"}, map[string]string{"path": "string", "pages": "string"})),
			Op("document_create", "Generate Word, spreadsheet, presentation, PDF or OpenDocument files from a structured specification", "write_local", "keyed", "after_completion", false, "documents", []string{"document", "create", "generate", "word", "pdf"}, ObjSchema([]string{"format", "spec"}, map[string]string{"format": "string", "spec": "string", "path": "string"})),
			Op("document_edit", "Apply structured operations on office documents: text, styles, tables, formulas, slides, comments", "write_local", "keyed", "after_completion", false, "documents", []string{"document", "edit", "modify", "table", "style"}, ObjSchema([]string{"path", "ops"}, map[string]string{"path": "string", "ops": "string"})),
			Op("pdf_ops", "Combine, split, rotate, extract pages, fill forms, annotate and redact PDF content", "write_local", "keyed", "after_completion", false, "documents", []string{"pdf", "merge", "split", "rotate", "redact"}, ObjSchema([]string{"action", "path"}, map[string]string{"action": "string", "path": "string", "pages": "string"})),
			Op("file_convert", "Convert between formats with the manifest pair list only", "write_local", "idempotent", "after_completion", false, "documents", []string{"convert", "format", "docx", "pdf"}, ObjSchema([]string{"from", "to", "path"}, map[string]string{"from": "string", "to": "string", "path": "string"})),
			Op("library_ingest", "Add a document to the library: keep the versioned original, chunk and index it", "write_local", "keyed", "after_completion", false, "documents", []string{"library", "ingest", "index", "version"}, ObjSchema([]string{"path"}, map[string]string{"path": "string", "scope": "string"})),
			Op("userdb", "Manage user-owned databases apart from the internal ones: create, schema, migrate, query, import, export", "write_local", "keyed", "after_completion", false, "documents", []string{"database", "userdb", "sql", "sqlite", "schema"}, ObjSchema([]string{"action"}, map[string]string{"action": "string", "db": "string", "sql": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// MediaManifest publishes diagram, chart and image operations.
func MediaManifest() Manifest {
	return Manifest{
		ID: "media", Version: "1",
		Operations: []Operation{
			Op("diagram_render", "Render a diagram from Mermaid, PlantUML, Graphviz, SVG or draw.io to SVG with concrete syntax errors", "read_only", "pure", "immediate", false, "media", []string{"diagram", "render", "mermaid", "graphviz"}, ObjSchema([]string{"dialect", "source"}, map[string]string{"dialect": "string", "source": "string", "format": "string"})),
			Op("chart_render", "Generate a chart from tabular data and a specification", "read_only", "pure", "immediate", false, "media", []string{"chart", "render", "bar", "line", "plot"}, ObjSchema([]string{"kind", "data"}, map[string]string{"kind": "string", "data": "string"})),
			Op("image_generate", "Generate images with the configured model, reporting the cost", "write_external", "keyed", "cooperative", false, "media", []string{"image", "generate", "picture"}, ObjSchema([]string{"prompt"}, map[string]string{"prompt": "string", "size": "string"})),
			Op("image_edit", "Edit an image: crop, resize, background removal, compose, retouch", "write_external", "keyed", "cooperative", false, "media", []string{"image", "edit", "crop", "resize"}, ObjSchema([]string{"path", "ops"}, map[string]string{"path": "string", "ops": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// DocumentVersion is one kept version of a user document, mirroring
// the Google-Docs-style history of Section 14.7.
type DocumentVersion struct {
	Version int    `json:"version"`
	Content string `json:"content"`
}

// UserTable is one in-memory user table of the userdb simulation.
type UserTable struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Version int        `json:"schema_version"`
}

// DocumentsFake is the deterministic documents simulation with
// versioned documents, an allowlisted converter and in-memory user
// databases.
type DocumentsFake struct {
	mu       sync.Mutex
	docs     map[string][]DocumentVersion
	dbTables map[string]map[string]*UserTable
	dedup    KeyDedup
	calls    map[string]int
	effects  map[string]int
	seq      int
}

// NewDocumentsFake returns a documents simulation.
func NewDocumentsFake() *DocumentsFake {
	return &DocumentsFake{docs: map[string][]DocumentVersion{}, dbTables: map[string]map[string]*UserTable{}, calls: map[string]int{}, effects: map[string]int{}}
}

// Manifest implements Capability.
func (d *DocumentsFake) Manifest() Manifest { return DocumentsManifest() }

// Effects counts first-effect executions per operation.
func (d *DocumentsFake) Effects(op string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.effects[op]
}

// Versions returns the kept versions of a document path.
func (d *DocumentsFake) Versions(path string) []DocumentVersion {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]DocumentVersion(nil), d.docs[path]...)
}

func (d *DocumentsFake) first(op, key string) {
	if d.dedup.First(op + "|" + key) {
		d.effects[op]++
	}
}

// Invoke implements Capability.
func (d *DocumentsFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls[operation]++
	switch operation {
	case "document_read":
		return d.read(args)
	case "document_create":
		return d.create(args, key)
	case "document_edit":
		return d.edit(args, key)
	case "pdf_ops":
		return d.pdf(args, key)
	case "file_convert":
		return d.convert(args)
	case "library_ingest":
		return d.ingest(args, key)
	case "userdb":
		return d.userdb(args, key)
	default:
		return Failed("", fmt.Sprintf("documents: unknown operation %q", operation), ErrInput), nil
	}
}

func (d *DocumentsFake) read(args json.RawMessage) (TaskResult, error) {
	path := argString(args, "path")
	if path == "" {
		return Failed("", "documents: path is required", ErrInput), nil
	}
	versions := d.docs[path]
	text := "placeholder content of " + path
	if len(versions) > 0 {
		text = versions[len(versions)-1].Content
	}
	return Completed("doc-"+path, map[string]any{
		"path": path, "format": formatOf(path), "version": len(versions),
		"pages":    []map[string]any{{"page": 1, "paragraphs": []string{text}, "text": text}},
		"sections": []string{},
	}), nil
}

func (d *DocumentsFake) create(args json.RawMessage, key string) (TaskResult, error) {
	format := argString(args, "format")
	spec := argString(args, "spec")
	if format == "" || spec == "" {
		return Failed("", "documents: format and spec are required", ErrInput), nil
	}
	d.first("document_create", key)
	path := argString(args, "path")
	if path == "" {
		d.seq++
		path = fmt.Sprintf("doc-%d.%s", d.seq, format)
	}
	d.docs[path] = []DocumentVersion{{Version: 1, Content: spec}}
	return Completed(path, map[string]any{"path": path, "format": format, "version": 1}), nil
}

func (d *DocumentsFake) edit(args json.RawMessage, key string) (TaskResult, error) {
	path := argString(args, "path")
	ops := argString(args, "ops")
	if path == "" || ops == "" {
		return Failed("", "documents: path and ops are required", ErrInput), nil
	}
	d.first("document_edit", key)
	versions := d.docs[path]
	versions = append(versions, DocumentVersion{Version: len(versions) + 1, Content: ops})
	d.docs[path] = versions
	return Completed(path, map[string]any{"path": path, "version": len(versions)}), nil
}

func (d *DocumentsFake) pdf(args json.RawMessage, key string) (TaskResult, error) {
	action := argString(args, "action")
	path := argString(args, "path")
	if action == "" || path == "" {
		return Failed("", "documents: action and path are required", ErrInput), nil
	}
	switch action {
	case "merge", "split", "rotate", "extract", "fill", "annotate", "redact":
	default:
		return Failed("", fmt.Sprintf("documents: unknown pdf action %q", action), ErrInput), nil
	}
	d.first("pdf_ops", key)
	return Completed("pdf-1", map[string]any{"action": action, "path": path, "pages": 1}), nil
}

func (d *DocumentsFake) convert(args json.RawMessage) (TaskResult, error) {
	from := strings.ToLower(argString(args, "from"))
	to := strings.ToLower(argString(args, "to"))
	path := argString(args, "path")
	if from == "" || to == "" || path == "" {
		return Failed("", "documents: from, to and path are required", ErrInput), nil
	}
	pair := from + "->" + to
	for _, allowed := range ConvertPairs {
		if allowed == pair {
			return Completed("conv-1", map[string]any{"from": from, "to": to, "path": path, "output": path + "." + to}), nil
		}
	}
	return Failed("", fmt.Sprintf("documents: unsupported conversion %q (supported: %s)", pair, strings.Join(ConvertPairs, ", ")), ErrInput), nil
}

func (d *DocumentsFake) ingest(args json.RawMessage, key string) (TaskResult, error) {
	path := argString(args, "path")
	if path == "" {
		return Failed("", "documents: path is required", ErrInput), nil
	}
	d.first("library_ingest", key)
	d.seq++
	return Completed(fmt.Sprintf("lib-%d", d.seq), map[string]any{"path": path, "version": 1, "doc_id": fmt.Sprintf("lib-%d", d.seq)}), nil
}

func (d *DocumentsFake) userdb(args json.RawMessage, key string) (TaskResult, error) {
	action := argString(args, "action")
	db := argString(args, "db")
	if action == "" || db == "" {
		return Failed("", "documents: action and db are required", ErrInput), nil
	}
	if d.dbTables[db] == nil {
		d.dbTables[db] = map[string]*UserTable{}
	}
	tables := d.dbTables[db]
	switch action {
	case "create", "schema", "migrate":
		d.first("userdb", key)
		sql := argString(args, "sql")
		name := tableName(sql)
		if _, ok := tables[name]; !ok {
			tables[name] = &UserTable{Columns: []string{"id"}, Version: 1}
		} else if action == "migrate" {
			tables[name].Version++
		}
		return Completed(db, map[string]any{"db": db, "table": name, "schema_version": tables[name].Version}), nil
	case "query":
		sql := argString(args, "sql")
		name := tableName(sql)
		t, ok := tables[name]
		if !ok {
			return Failed("", fmt.Sprintf("documents: unknown table %q", name), ErrInput), nil
		}
		return Completed(db, map[string]any{"rows": t.Rows, "columns": t.Columns}), nil
	case "import":
		d.first("userdb", key)
		return Completed(db, map[string]any{"db": db, "imported": true}), nil
	case "export":
		return Completed(db, map[string]any{"db": db, "tables": tableNames(tables)}), nil
	default:
		return Failed("", fmt.Sprintf("documents: unknown userdb action %q", action), ErrInput), nil
	}
}

// Poll implements Capability.
func (d *DocumentsFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (d *DocumentsFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (d *DocumentsFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return d.Poll(taskID)
}

func formatOf(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 && i+1 < len(path) {
		return strings.ToLower(path[i+1:])
	}
	return "txt"
}

func tableName(sql string) string {
	fields := strings.Fields(strings.ToLower(sql))
	for i, f := range fields {
		if f == "table" && i+1 < len(fields) {
			return strings.Trim(fields[i+1], "`\"();")
		}
		if f == "from" && i+1 < len(fields) {
			return strings.Trim(fields[i+1], "`\"();")
		}
		if f == "into" && i+1 < len(fields) {
			return strings.Trim(fields[i+1], "`\"();")
		}
	}
	return "t"
}

func tableNames(tables map[string]*UserTable) []string {
	out := []string{}
	for name := range tables {
		out = append(out, name)
	}
	return out
}

// MediaFake is the deterministic media simulation: SVG rendering
// targets with concrete syntax errors, and image generation reporting
// its cost.
type MediaFake struct {
	mu     sync.Mutex
	dedup  KeyDedup
	calls  map[string]int
	images int
}

// NewMediaFake returns a media simulation.
func NewMediaFake() *MediaFake {
	return &MediaFake{calls: map[string]int{}}
}

// Manifest implements Capability.
func (m *MediaFake) Manifest() Manifest { return MediaManifest() }

// Invoke implements Capability.
func (m *MediaFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls[operation]++
	switch operation {
	case "diagram_render":
		dialect := strings.ToLower(argString(args, "dialect"))
		source := argString(args, "source")
		format := strings.ToLower(argString(args, "format"))
		if format == "" {
			format = "svg"
		}
		if format != "svg" {
			return Failed("", fmt.Sprintf("media: dialect %q renders to SVG only; %q needs a rasterizer", dialect, format), ErrInput), nil
		}
		switch dialect {
		case "mermaid", "plantuml", "graphviz", "svg", "drawio":
		default:
			return Failed("", fmt.Sprintf("media: unknown diagram dialect %q", dialect), ErrInput), nil
		}
		if strings.TrimSpace(source) == "" {
			return Failed("", fmt.Sprintf("media: %s source is empty", dialect), ErrInput), nil
		}
		if strings.Contains(source, "syntax-error") {
			return Failed("", fmt.Sprintf("media: %s syntax error near %q", dialect, "syntax-error"), ErrInput), nil
		}
		return Completed("diag-1", map[string]any{"svg": "<svg>" + dialect + "</svg>"}), nil
	case "chart_render":
		kind := strings.ToLower(argString(args, "kind"))
		switch kind {
		case "bar", "line":
		default:
			return Failed("", fmt.Sprintf("media: unknown chart kind %q (want bar or line)", kind), ErrInput), nil
		}
		return Completed("chart-1", map[string]any{"svg": "<svg>chart:" + kind + "</svg>"}), nil
	case "image_generate":
		prompt := argString(args, "prompt")
		if prompt == "" {
			return Failed("", "media: prompt is required", ErrInput), nil
		}
		if !m.dedup.First(operation + "|" + key) {
			return Completed("img-cached", map[string]any{"cached": true, "cost_usd": 0.0}), nil
		}
		m.images++
		return Completed(fmt.Sprintf("img-%d", m.images), map[string]any{"image_ref": fmt.Sprintf("blob:img-%d", m.images), "cost_usd": 0.04}), nil
	case "image_edit":
		if argString(args, "path") == "" {
			return Failed("", "media: path is required", ErrInput), nil
		}
		if !m.dedup.First(operation + "|" + key) {
			return Completed("imgedit-cached", map[string]any{"cached": true}), nil
		}
		return Completed("imgedit-1", map[string]any{"image_ref": "blob:imgedit-1"}), nil
	default:
		return Failed("", fmt.Sprintf("media: unknown operation %q", operation), ErrInput), nil
	}
}

// Poll implements Capability.
func (m *MediaFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (m *MediaFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (m *MediaFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return m.Poll(taskID)
}
