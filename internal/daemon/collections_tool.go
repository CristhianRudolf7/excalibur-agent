package daemon

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

//go:embed collections_tool_schema.json
var collectionToolSchema json.RawMessage

type collectionTool struct {
	backend  *DaemonBackend
	services *Services
}

func (t *collectionTool) service() *CollectionRoutes {
	if t.backend != nil && t.backend.Services != nil && t.backend.Services.Collections != nil {
		return t.backend.Services.Collections
	}
	if t.services != nil {
		return t.services.Collections
	}
	return nil
}
func (t *collectionTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "my_data", Group: "collections", Profiles: []string{"default"}, Effect: types.EffectWriteLocal, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelCooperative, Timeout: 60 * time.Second, OutputBudget: 24000,
		Keywords:    []string{"my data", "Mis datos", "collections", "database", "grades", "income", "tracking", "CSV", "Excel", "colecciones", "notas", "ingresos", "seguimiento"},
		Description: `Manage the user's personal collections (My data / Mis datos), fields, relations, records, spreadsheet imports and exports. Use list/describe to discover collection and stable field IDs. Use collection for the parent collection and id for a record; collection operations accept id. Never run SQL. When the user wants to track or manage information, FIRST call propose and show its schema table or fictional sample rows, then ask whether to create it. Only call create with confirmed:true after their confirmation. Never treat sample rows as actual user records. Creating collections, deleting whole collections or fields, and potentially lossy type changes require confirmation. Record insert/update/delete need no confirmation. After creation, say it appears under "Mis datos" (Spanish) or "My data" (English), matching the user's language. If the user attaches Excel/CSV and talks about tracking/managing data, offer to turn it into a collection. For a spreadsheet that has no collection yet, FIRST call import with its blob_digest, no collection, create_schema:true and no commit (a dry run that creates nothing): base the proposal on the returned proposed_fields and report (real columns, conflicts, fixes), never on guessed columns. Import is a dry run by default: use the attachment blob_digest, inspect proposed_fields and report; fixes:auto trims, normalizes decimal commas, unambiguous dates and select casing. Report what was fixed. Use commit:true to apply valid rows. To apply a confirmed spreadsheet proposal, call create (confirmed:true) with the proposed fields plus every constraint the user confirmed (min/max, required, options), then import into that collection id with commit:true; create_schema:true imports only the inferred fields and would drop confirmed constraints. Never claim a constraint that describe does not show. Never invent missing values. For many or ambiguous conflicts, import the valid rows and ask concise concrete questions identifying remaining row/column/value problems. When proposing, add obvious domain constraints (for example a min/max grading scale) and ask the user to confirm them. fixes:auto also converts number words. Skipped rows come back in report.pending_rows with every original cell. When the user clarifies skipped rows, re-run import of the same blob_digest with overrides:[{row, column, value}] (row numbers as reported, column = source header, value = the clarified value, e.g. an ISO date) and skip_rows for rows the user wants left out, first as a dry run and then with commit:true; identical rows already imported are skipped automatically as duplicates, so only the clarified rows are added with all their other cells. Earlier tool results are not kept between turns, so run the dry run again to see current pending_rows. Leave rows the user did not clarify pending and say which. Re-run a dry run after resolving mapping or values. Exports return a downloadable blob URL. Restore with version restores that schema AND its saved records and requires confirmed:true; without version it restores a trashed collection. list with include_trash:true finds trashed IDs. Outputs include stable IDs; translate explanations into the user's language.`,
		InputSchema: collectionToolSchema,
	}
}
func (t *collectionTool) ResourceKeys(json.RawMessage) ([]string, error) {
	return []string{"personal-collections"}, nil
}
func collectionToolResult(value any, err error) (tools.Result, error) {
	if err != nil {
		raw, _ := json.Marshal(collectionError(err))
		return tools.Result{Status: tools.StatusInputError, Output: string(raw)}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, nil
}
func collectionConfirmation() (tools.Result, error) {
	return tools.Result{Status: tools.StatusDenied, Output: `{"confirmation_required":true,"instruction":{"en":"Show the proposal or destructive changes and ask the user to confirm before retrying with confirmed:true.","es":"Muestra la propuesta o los cambios destructivos y pide confirmación antes de reintentar con confirmed:true."}}`}, nil
}
func (t *collectionTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	s := t.service()
	if s == nil || s.Engine == nil {
		return collectionToolResult(nil, fmt.Errorf("collections store unavailable"))
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(inv.Arguments, &args); err != nil || args == nil {
		return collectionToolResult(nil, fmt.Errorf("expected object arguments"))
	}
	var action string
	var confirmed, commit, includeTrash bool
	var version int
	for key, target := range map[string]any{"action": &action, "confirmed": &confirmed, "commit": &commit, "include_trash": &includeTrash, "version": &version} {
		if raw, ok := args[key]; ok {
			if err := json.Unmarshal(raw, target); err != nil {
				return collectionToolResult(nil, err)
			}
		}
		delete(args, key)
	}
	var id, collection string
	_ = json.Unmarshal(args["id"], &id)
	_ = json.Unmarshal(args["collection"], &collection)
	collectionAction := map[string]bool{"describe": true, "alter_fields": true, "delete_collection": true, "summary": true, "export": true, "restore": true}[action]
	if collectionAction && id == "" {
		id = collection
		args["id"], _ = json.Marshal(id)
	}
	if collectionAction {
		delete(args, "collection")
	}
	raw, _ := json.Marshal(args)
	switch action {
	case "propose":
		var p api.CollectionsCreateParams
		if err := decodeCollections(raw, &p); err != nil {
			return collectionToolResult(nil, err)
		}
		proposal, err := s.Engine.Propose(ctx, p)
		if err != nil {
			return collectionToolResult(nil, err)
		}
		return collectionToolResult(map[string]any{"proposal": proposal, "schema_table": schemaTable(proposal), "example_rows": exampleRows(proposal), "examples_are_fictional": true, "requires_confirmation": true}, nil)
	case "create":
		if !confirmed {
			return collectionConfirmation()
		}
	case "alter_fields":
		var p api.CollectionsFieldsParams
		if err := decodeCollections(raw, &p); err != nil {
			return collectionToolResult(nil, err)
		}
		if !p.DryRun && (len(p.Remove) > 0 || len(p.ChangeType) > 0) && !confirmed {
			return collectionConfirmation()
		}
	case "delete_collection":
		if !confirmed {
			return collectionConfirmation()
		}
	case "restore":
		if version > 0 && !confirmed {
			return collectionConfirmation()
		}
		c, err := s.Engine.Restore(ctx, id, version)
		return collectionToolResult(api.CollectionResult{Collection: c}, err)
	case "list":
		if includeTrash {
			list, err := s.Engine.List(ctx)
			if err != nil {
				return collectionToolResult(nil, err)
			}
			trash, err := s.Engine.Trash(ctx)
			return collectionToolResult(map[string]any{"collections": list, "trash": trash}, err)
		}
	case "import":
		var p api.CollectionsImportParams
		if err := decodeCollections(raw, &p); err != nil {
			return collectionToolResult(nil, err)
		}
		p.DryRun = !commit
		if commit && p.Collection == "" && p.CreateSchema && !confirmed {
			return collectionConfirmation()
		}
		if p.Fixes == "" {
			p.Fixes = "auto"
		}
		out, err := s.Engine.Import(ctx, s.Blobs, p)
		return collectionToolResult(out, err)
	}
	methods := map[string]string{"list": api.MethodCollectionsList, "describe": api.MethodCollectionsGet, "create": api.MethodCollectionsCreate, "alter_fields": api.MethodCollectionsFields, "delete_collection": api.MethodCollectionsDelete, "insert": api.MethodRecordsCreate, "update": api.MethodRecordsUpdate, "delete_record": api.MethodRecordsDelete, "query": api.MethodRecordsList, "summary": api.MethodCollectionsSummary, "export": api.MethodCollectionsExport}
	method, ok := methods[action]
	if !ok {
		return collectionToolResult(nil, fmt.Errorf("unknown my_data action"))
	}
	value, err := s.Op(ctx, method, raw)
	if err != nil {
		return collectionToolResult(nil, err)
	}
	if action == "describe" {
		versions, err := s.Engine.Versions(ctx, id)
		if err != nil {
			return collectionToolResult(nil, err)
		}
		return collectionToolResult(map[string]any{"collection": value.(api.CollectionResult).Collection, "schema_versions": versions}, nil)
	}
	if action == "create" {
		return collectionToolResult(map[string]any{"collection": value.(api.CollectionResult).Collection, "notice": map[string]string{"en": "Created. It appears under My data.", "es": "Creada. Aparece en Mis datos."}}, nil)
	}
	if action == "export" {
		out := value.(api.CollectionsExportResult)
		return collectionToolResult(map[string]any{"blob_digest": out.BlobDigest, "name": out.Name, "mime": out.Mime, "url": "/blobs/" + out.BlobDigest + "?mime=" + url.QueryEscape(out.Mime)}, nil)
	}
	return collectionToolResult(value, nil)
}
func schemaTable(c api.Collection) string {
	var b strings.Builder
	b.WriteString("| Field | ID | Type | Required | Unique | Details |\n| --- | --- | --- | --- | --- | --- |\n")
	safe := func(s string) string { return strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(s) }
	for _, f := range c.Fields {
		details := f.Currency
		if f.Target != "" {
			details = f.Target
		}
		if len(f.Options) > 0 {
			details = strings.Join(f.Options, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %t | %t | %s |\n", safe(f.Name), f.ID, f.Type, f.Required, f.Unique, safe(details))
	}
	return b.String()
}
func exampleRows(c api.Collection) []map[string]any {
	rows := []map[string]any{}
	for i := 1; i <= 3; i++ {
		r := map[string]any{}
		for _, f := range c.Fields {
			var value any
			switch f.Type {
			case "text", "long_text":
				value = fmt.Sprintf("Example %d", i)
			case "number", "integer", "money":
				value = i * 10
			case "boolean":
				value = i%2 == 1
			case "date":
				value = fmt.Sprintf("2026-01-0%d", i)
			case "datetime":
				value = fmt.Sprintf("2026-01-0%dT12:00:00Z", i)
			case "select":
				if len(f.Options) > 0 {
					value = f.Options[(i-1)%len(f.Options)]
				}
			case "multi_select":
				if len(f.Options) > 0 {
					value = []string{f.Options[0]}
				}
			case "email":
				value = fmt.Sprintf("example%d@example.test", i)
			case "url":
				value = "https://example.test"
			}
			r[f.ID] = value
		}
		rows = append(rows, r)
	}
	return rows
}
func wireCollectionTool(b *DaemonBackend, s *Services) error {
	if s == nil || s.Tools == nil {
		return nil
	}
	if _, err := s.Tools.Get("my_data"); err == nil {
		return nil
	}
	if err := s.Tools.Register(&collectionTool{backend: b, services: s}); err != nil {
		return err
	}
	if s.Catalog != nil {
		s.Catalog.MarkDeferred("my_data")
	}
	return nil
}

var _ tools.Tool = (*collectionTool)(nil)
