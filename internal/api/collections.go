package api

const (
	MethodCollectionsList    = "collections.list"
	MethodCollectionsGet     = "collections.get"
	MethodCollectionsCreate  = "collections.create"
	MethodCollectionsUpdate  = "collections.update"
	MethodCollectionsFields  = "collections.fields"
	MethodCollectionsDelete  = "collections.delete"
	MethodCollectionsSummary = "collections.summary"
	MethodCollectionsImport  = "collections.import"
	MethodCollectionsExport  = "collections.export"
	MethodRecordsList        = "records.list"
	MethodRecordsGet         = "records.get"
	MethodRecordsCreate      = "records.create"
	MethodRecordsUpdate      = "records.update"
	MethodRecordsDelete      = "records.delete"
	EventCollectionsChanged  = "collections.changed"
)

type FieldType string

type CollectionField struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required,omitempty"`
	Unique      bool      `json:"unique,omitempty"`
	Options     []string  `json:"options,omitempty"`
	Currency    string    `json:"currency,omitempty"`
	Target      string    `json:"target,omitempty"`
	Min         *float64  `json:"min,omitempty"`
	Max         *float64  `json:"max,omitempty"`
	Description string    `json:"description,omitempty"`
}
type CollectionFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}
type SummaryCard struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Kind        string             `json:"kind"`
	Field       string             `json:"field,omitempty"`
	WeightField string             `json:"weight_field,omitempty"`
	Filter      []CollectionFilter `json:"filter,omitempty"`
	GroupBy     string             `json:"group_by,omitempty"`
	Chart       string             `json:"chart,omitempty"`
}
type Collection struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Icon         string            `json:"icon,omitempty"`
	Description  string            `json:"description,omitempty"`
	Fields       []CollectionField `json:"fields"`
	DisplayField string            `json:"display_field"`
	Summary      []SummaryCard     `json:"summary,omitempty"`
	RecordCount  int               `json:"record_count"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
	Version      int               `json:"version"`
}
type RecordRow struct {
	ID        string         `json:"id"`
	Values    map[string]any `json:"values"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}
type CollectionsCreateParams struct {
	Name         string            `json:"name"`
	Icon         string            `json:"icon,omitempty"`
	Description  string            `json:"description,omitempty"`
	Fields       []CollectionField `json:"fields"`
	DisplayField string            `json:"display_field,omitempty"`
	Summary      []SummaryCard     `json:"summary,omitempty"`
}
type CollectionsIDParams struct {
	ID string `json:"id"`
}
type CollectionsUpdateParams struct {
	ID           string         `json:"id"`
	Name         *string        `json:"name,omitempty"`
	Icon         *string        `json:"icon,omitempty"`
	Description  *string        `json:"description,omitempty"`
	Summary      *[]SummaryCard `json:"summary,omitempty"`
	DisplayField *string        `json:"display_field,omitempty"`
}
type FieldRename struct {
	Field string `json:"field"`
	Name  string `json:"name"`
}
type FieldTypeChange struct {
	Field   string    `json:"field"`
	Type    FieldType `json:"type"`
	Options []string  `json:"options,omitempty"`
}
type CollectionsFieldsParams struct {
	ID         string            `json:"id"`
	Add        []CollectionField `json:"add,omitempty"`
	Rename     []FieldRename     `json:"rename,omitempty"`
	Remove     []string          `json:"remove,omitempty"`
	ChangeType []FieldTypeChange `json:"change_type,omitempty"`
	DryRun     bool              `json:"dry_run,omitempty"`
}
type ConversionFailure struct {
	RecordID string `json:"record_id"`
	Value    any    `json:"value"`
}
type FieldConversion struct {
	Field     string              `json:"field"`
	Converted int                 `json:"converted"`
	Failed    []ConversionFailure `json:"failed"`
}
type CollectionsFieldsResult struct {
	Collection Collection        `json:"collection"`
	Report     []FieldConversion `json:"report,omitempty"`
}
type RecordsListParams struct {
	Collection string             `json:"collection"`
	Query      string             `json:"query,omitempty"`
	Filters    []CollectionFilter `json:"filters,omitempty"`
	Sort       []RecordSort       `json:"sort,omitempty"`
	Limit      int                `json:"limit,omitempty"`
	Cursor     string             `json:"cursor,omitempty"`
}
type RecordSort struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}
type RecordsListResult struct {
	Records    []RecordRow `json:"records"`
	Total      int         `json:"total"`
	NextCursor string      `json:"next_cursor,omitempty"`
}
type RecordsIDParams struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
}
type RecordsCreateParams struct {
	Collection string         `json:"collection"`
	Values     map[string]any `json:"values"`
}
type RecordsUpdateParams struct {
	Collection string         `json:"collection"`
	ID         string         `json:"id"`
	Values     map[string]any `json:"values"`
}
type RelatedRecords struct {
	Collection string      `json:"collection"`
	Field      string      `json:"field"`
	Records    []RecordRow `json:"records"`
}
type RecordsGetResult struct {
	Record  RecordRow        `json:"record"`
	Related []RelatedRecords `json:"related"`
}
type SummarySeries struct {
	Label string `json:"label"`
	Value any    `json:"value"`
}
type SummaryValue struct {
	ID     string          `json:"id"`
	Title  string          `json:"title"`
	Value  any             `json:"value"`
	Unit   string          `json:"unit,omitempty"`
	Series []SummarySeries `json:"series,omitempty"`
}
type ImportMapping struct {
	Column string  `json:"column"`
	Field  *string `json:"field"`
}
type ImportConflict struct {
	Row        int    `json:"row"`
	Column     string `json:"column"`
	Value      string `json:"value"`
	Problem    string `json:"problem"`
	Suggestion string `json:"suggestion,omitempty"`
}
type ImportFix struct {
	Row    int    `json:"row"`
	Column string `json:"column"`
	From   string `json:"from"`
	To     string `json:"to"`
	Rule   string `json:"rule"`
}
type ImportReport struct {
	DryRun         bool             `json:"dry_run"`
	TotalRows      int              `json:"total_rows"`
	Imported       int              `json:"imported"`
	Skipped        int              `json:"skipped"`
	Updated        int              `json:"updated"`
	Mapping        []ImportMapping  `json:"mapping"`
	UnknownColumns []string         `json:"unknown_columns"`
	Conflicts      []ImportConflict `json:"conflicts"`
	FixesApplied   []ImportFix      `json:"fixes_applied"`
	// PendingRows carries every skipped row with all its original cells so a
	// follow-up correction can re-insert it without losing other columns.
	PendingRows []ImportPendingRow `json:"pending_rows,omitempty"`
}

// ImportPendingRow is one skipped source row, keyed by source column.
type ImportPendingRow struct {
	Row    int               `json:"row"`
	Values map[string]string `json:"values"`
	Reason string            `json:"reason"`
}
type CollectionsImportParams struct {
	Collection   string          `json:"collection,omitempty"`
	BlobDigest   string          `json:"blob_digest"`
	Sheet        string          `json:"sheet,omitempty"`
	Mapping      []ImportMapping `json:"mapping,omitempty"`
	CreateSchema bool            `json:"create_schema,omitempty"`
	UpsertKey    string          `json:"upsert_key,omitempty"`
	Fixes        string          `json:"fixes,omitempty"`
	DryRun       bool            `json:"dry_run,omitempty"`
}
type CollectionsImportResult struct {
	Collection     *Collection       `json:"collection,omitempty"`
	Report         ImportReport      `json:"report"`
	ProposedFields []CollectionField `json:"proposed_fields,omitempty"`
}
type CollectionsExportParams struct {
	ID      string             `json:"id"`
	Format  string             `json:"format"`
	Filters []CollectionFilter `json:"filters,omitempty"`
}
type CollectionsExportResult struct {
	BlobDigest string `json:"blob_digest"`
	Name       string `json:"name"`
	Mime       string `json:"mime"`
}
type CollectionsChangedEvent struct {
	Collection string `json:"collection"`
	RecordID   string `json:"record_id,omitempty"`
	Kind       string `json:"kind"`
}
type CollectionsListResult struct {
	Collections []Collection `json:"collections"`
}
type CollectionResult struct {
	Collection Collection `json:"collection"`
}
type RecordResult struct {
	Record RecordRow `json:"record"`
}
type CollectionsSummaryResult struct {
	Cards []SummaryValue `json:"cards"`
}
