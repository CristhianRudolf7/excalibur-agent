package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/edit"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/opc"
)

// OfficeWriteEnabled keeps discovery stable while allowing local writes to be disabled.
func OfficeWriteEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_OFFICE_WRITE"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// DocumentCreateTool creates basic editable DOCX/XLSX/PPTX files from a JSON spec.
type DocumentCreateTool struct{ Deps *MediaDeps }

// DocumentEditTool applies guarded format-preserving operations to Office files.
type DocumentEditTool struct{ Deps *MediaDeps }

func officeDefinition(name, description string, schema json.RawMessage) Definition {
	return Definition{Name: name, Description: description, InputSchema: schema, Effect: "write_local", Idempotency: "non_idempotent", Cancel: "after_completion", Timeout: 30 * time.Second, OutputBudget: DefaultOutputBudget, Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"}, Group: "documents", Tags: []string{"office", "docx", "xlsx", "pptx"}, Keywords: []string{"document", "word", "spreadsheet", "presentation", "create", "edit"}}
}
func (t *DocumentCreateTool) Definition() Definition {
	return officeDefinition("document_create", "Create an editable DOCX, XLSX or PPTX from a structured JSON spec; returns sha256, anchors and validation issues", officeSchema(false))
}
func (t *DocumentEditTool) Definition() Definition {
	d := officeDefinition("document_edit", "Edit Office text, paragraphs, tables, cells, sheets, slides and notes while preserving formatting; requires expected_sha256 from the original file", officeSchema(true))
	d.Idempotency = "idempotent"
	return d
}
func officePath(deps *MediaDeps, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("path is required")
	}
	root := ""
	if deps != nil {
		root = deps.Workdir
	}
	if root == "" {
		var e error
		root, e = os.Getwd()
		if e != nil {
			return "", e
		}
	}
	root, e := filepath.Abs(root)
	if e != nil {
		return "", e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p, e = filepath.Abs(p)
	if e != nil {
		return "", e
	}
	resolved, e := filepath.EvalSymlinks(p)
	if os.IsNotExist(e) {
		parent, pe := filepath.EvalSymlinks(filepath.Dir(p))
		if pe != nil {
			return "", pe
		}
		resolved = filepath.Join(parent, filepath.Base(p))
		e = nil
	}
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, resolved)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must stay inside the run workspace")
	}
	return resolved, nil
}
func officeError(err error, issues []string) Result {
	raw, _ := json.Marshal(struct {
		Error  string   `json:"error"`
		Issues []string `json:"issues,omitempty"`
	}{err.Error(), issues})
	return Result{Status: StatusInputError, Output: string(raw)}
}
func officeDecode(raw json.RawMessage, v any) error {
	if len(raw) > 4<<20 {
		return fmt.Errorf("arguments exceed 4 MiB")
	}
	return json.Unmarshal(raw, v)
}

// runtimeInputSchema accepts presentation extensions without changing the
// pinned, model-visible document_create definition. Other formats keep the
// original validation, and unknown properties remain errors.
func (t *DocumentCreateTool) runtimeInputSchema(args json.RawMessage) json.RawMessage {
	base := officeSchema(false)
	var request struct {
		Format string `json:"format"`
	}
	if json.Unmarshal(args, &request) != nil || request.Format != "pptx" {
		return base
	}
	var schema map[string]any
	_ = json.Unmarshal(base, &schema)
	spec := schema["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)
	str := map[string]any{"type": "string"}
	spec["theme"] = map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"accent": str,
			"style":  map[string]any{"type": "string", "enum": []string{"light", "dark"}},
		},
	}
	slide := spec["slides"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	slide["kind"] = map[string]any{"type": "string", "enum": []string{"title", "bullets", "two_column", "image", "table", "section", "cards", "quote"}}
	for _, field := range []string{"caption", "quote", "attribution"} {
		slide[field] = str
	}
	slide["cards"] = map[string]any{
		"type": "array", "minItems": 2, "maxItems": 4,
		"items": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"title", "text"},
			"properties": map[string]any{"title": str, "text": str, "value": str},
		},
	}
	runtimeSchema, _ := json.Marshal(schema)
	return runtimeSchema
}

func (t *DocumentCreateTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	if !OfficeWriteEnabled() {
		return Result{Status: StatusDenied, Output: "Office writes are disabled by EXCALIBUR_OFFICE_WRITE"}, nil
	}
	if e := ctx.Err(); e != nil {
		return Result{Status: StatusCanceled, Output: e.Error()}, nil
	}
	var a struct {
		Path      string      `json:"path"`
		Format    string      `json:"format"`
		Spec      create.Spec `json:"spec"`
		Overwrite bool        `json:"overwrite"`
	}
	if e := officeDecode(inv.Arguments, &a); e != nil {
		return officeError(e, nil), nil
	}
	filename, e := officePath(t.Deps, a.Path)
	if e != nil {
		return officeError(e, nil), nil
	}
	format := strings.ToLower(a.Format)
	if format != strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".") {
		return officeError(fmt.Errorf("format must match the file extension"), nil), nil
	}
	p, e := create.Build(format, a.Spec, func(name string) ([]byte, error) {
		p, e := officePath(t.Deps, name)
		if e != nil {
			return nil, e
		}
		st, e := os.Stat(p)
		if e != nil {
			return nil, e
		}
		if st.Size() > 16<<20 {
			return nil, fmt.Errorf("image too large")
		}
		return os.ReadFile(p)
	})
	if e != nil {
		return officeError(e, nil), nil
	}
	if e = ctx.Err(); e != nil {
		return Result{Status: StatusCanceled, Output: e.Error()}, nil
	}
	h, issues, e := opc.WriteAtomic(filename, p, "", a.Overwrite)
	if e != nil {
		return officeError(e, issues), nil
	}
	raw, _ := json.Marshal(edit.Result{SHA256: h, Summary: "created " + format, Anchors: edit.Anchors(p, format), Issues: issues})
	return Result{Status: StatusOK, Output: string(raw)}, nil
}
func (t *DocumentEditTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	if !OfficeWriteEnabled() {
		return Result{Status: StatusDenied, Output: "Office writes are disabled by EXCALIBUR_OFFICE_WRITE"}, nil
	}
	var a struct {
		Path     string           `json:"path"`
		Expected string           `json:"expected_sha256"`
		Ops      []edit.Operation `json:"ops"`
	}
	if e := officeDecode(inv.Arguments, &a); e != nil {
		return officeError(e, nil), nil
	}
	filename, e := officePath(t.Deps, a.Path)
	if e != nil {
		return officeError(e, nil), nil
	}
	res, e := edit.File(ctx, filename, a.Expected, a.Ops)
	if e != nil {
		if ctx.Err() != nil {
			return Result{Status: StatusCanceled, Output: ctx.Err().Error()}, nil
		}
		return officeError(e, res.Issues), nil
	}
	raw, _ := json.Marshal(res)
	return Result{Status: StatusOK, Output: string(raw)}, nil
}
func officeResources(deps *MediaDeps, raw json.RawMessage) ([]string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if e := json.Unmarshal(raw, &a); e != nil {
		return nil, e
	}
	p, e := officePath(deps, a.Path)
	if e != nil {
		return nil, e
	}
	return []string{"file:" + p}, nil
}
func (t *DocumentCreateTool) ResourceKeys(raw json.RawMessage) ([]string, error) {
	return officeResources(t.Deps, raw)
}
func (t *DocumentEditTool) ResourceKeys(raw json.RawMessage) ([]string, error) {
	return officeResources(t.Deps, raw)
}

// officeSchema describes the spec and operations at deferred discovery time.
func officeSchema(editing bool) json.RawMessage {
	typ := func(t string) map[string]any { return map[string]any{"type": t} }
	obj := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	}
	arr := func(items any) map[string]any { return map[string]any{"type": "array", "items": items} }
	enum := func(v ...string) map[string]any { return map[string]any{"type": "string", "enum": v} }
	str, num, integer, boolean := typ("string"), typ("number"), typ("integer"), typ("boolean")
	rows := arr(arr(str))
	run := obj(map[string]any{"text": str, "bold": boolean, "italic": boolean, "code": boolean}, "text")
	block := obj(map[string]any{"kind": enum("paragraph", "heading", "bullet", "numbered", "table", "page_break", "image"), "text": str, "runs": arr(run), "level": integer, "style": str, "rows": rows, "header": boolean, "image": str}, "kind")
	cell := obj(map[string]any{"ref": str, "type": enum("string", "number", "bool", "date"), "value": map[string]any{"type": []string{"string", "number", "boolean", "null"}}, "formula": str, "header": boolean}, "ref")
	sheet := obj(map[string]any{"name": str, "cells": arr(cell), "column_widths": arr(num), "freeze_first_row": boolean}, "name")
	bullet := obj(map[string]any{"text": str, "level": integer}, "text")
	slide := obj(map[string]any{"kind": enum("title", "bullets", "two_column", "image", "table"), "title": str, "subtitle": str, "bullets": arr(bullet), "left": arr(bullet), "right": arr(bullet), "image": str, "rows": rows, "notes": str}, "kind")
	spec := obj(map[string]any{"title": str, "page": obj(map[string]any{"size": enum("A4", "Letter"), "margin_twips": integer}), "blocks": arr(block), "sheets": arr(sheet), "slides": arr(slide)})
	op := obj(map[string]any{"action": enum("replace_text", "insert_paragraph", "delete_paragraph", "append_table", "set_cell", "add_sheet", "rename_sheet", "delete_rows", "set_notes", "duplicate_slide", "delete_slide", "move_slide"), "anchor": str, "match": str, "text": str, "all": boolean, "position": enum("before", "after"), "style": str, "rows": rows, "header": boolean, "sheet": str, "name": str, "cell": cell, "new_sheet": sheet, "start": integer, "count": integer, "slide": integer, "to": integer}, "action")
	schema := obj(map[string]any{"path": str, "format": enum("docx", "xlsx", "pptx"), "spec": spec, "overwrite": boolean}, "path", "format", "spec")
	if editing {
		schema = obj(map[string]any{"path": str, "expected_sha256": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}, "ops": arr(op)}, "path", "expected_sha256", "ops")
	}
	b, _ := json.Marshal(schema)
	return b
}
