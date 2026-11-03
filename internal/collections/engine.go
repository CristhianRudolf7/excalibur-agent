package collections

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const DefaultSchemaVersions = 10
const TrashRetention = 30 * 24 * time.Hour

var ErrNotFound = errors.New("collection or record not found")

type Engine struct {
	mu       sync.Mutex
	db       *sql.DB
	versions int
	changed  func(api.CollectionsChangedEvent)
}
type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func newID(prefix string) string { return prefix + strings.ReplaceAll(uuid.NewString(), "-", "") }
func now() string                { return time.Now().UTC().Format(time.RFC3339Nano) }

// quote only receives schema-validated field IDs or generated table IDs. Double
// quoting is still applied so no caller-controlled identifier becomes SQL.
func quote(s string) string     { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func table(c Collection) string { return quote("data_" + c.ID) }
func Open(dataDir string, changed func(api.CollectionsChangedEvent)) (*Engine, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "collections.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	e := &Engine{db: db, versions: DefaultSchemaVersions, changed: changed}
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;
 CREATE TABLE IF NOT EXISTS collection_meta(id TEXT PRIMARY KEY, schema_json TEXT NOT NULL, deleted_at TEXT);
 CREATE TABLE IF NOT EXISTS collection_fields(collection_id TEXT NOT NULL, field_id TEXT NOT NULL, ordinal INTEGER NOT NULL, schema_json TEXT NOT NULL, PRIMARY KEY(collection_id,field_id));
 CREATE TABLE IF NOT EXISTS schema_versions(collection_id TEXT NOT NULL, version INTEGER NOT NULL, snapshot TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(collection_id,version));
 PRAGMA user_version=1;`); err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0600); err == nil {
		err = e.purge(context.Background())
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return e, nil
}
func (e *Engine) Close() error {
	if e == nil {
		return nil
	}
	return e.db.Close()
}
func (e *Engine) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (e *Engine) emit(c, r, kind string) {
	if e.changed != nil {
		e.changed(api.CollectionsChangedEvent{Collection: c, RecordID: r, Kind: kind})
	}
}
func getCollection(ctx context.Context, q queryer, id string, trash bool) (Collection, error) {
	var c Collection
	var raw string
	sqlText := `SELECT schema_json FROM collection_meta WHERE id=?`
	if !trash {
		sqlText += ` AND deleted_at IS NULL`
	}
	err := q.QueryRowContext(ctx, sqlText, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal([]byte(raw), &c); err != nil {
		return c, err
	}
	err = q.QueryRowContext(ctx, `SELECT count(*) FROM `+table(c)).Scan(&c.RecordCount)
	return c, err
}
func listCollections(ctx context.Context, q queryer) ([]Collection, error) {
	rows, err := q.QueryContext(ctx, `SELECT schema_json FROM collection_meta WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	list := []Collection{}
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			rows.Close()
			return nil, err
		}
		var c Collection
		if err = json.Unmarshal([]byte(s), &c); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if err = q.QueryRowContext(ctx, `SELECT count(*) FROM `+table(list[i])).Scan(&list[i].RecordCount); err != nil {
			return nil, err
		}
	}
	return list, nil
}
func saveCollection(ctx context.Context, q queryer, c Collection) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, `INSERT INTO collection_meta(id,schema_json) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET schema_json=excluded.schema_json`, c.ID, string(raw)); err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, `DELETE FROM collection_fields WHERE collection_id=?`, c.ID); err != nil {
		return err
	}
	for i, f := range c.Fields {
		raw, err = json.Marshal(f)
		if err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, `INSERT INTO collection_fields VALUES(?,?,?,?)`, c.ID, f.ID, i, string(raw)); err != nil {
			return err
		}
	}
	return nil
}
func createTable(ctx context.Context, q queryer, c Collection) error {
	cols := []string{`id TEXT PRIMARY KEY`, `created_at TEXT NOT NULL`, `updated_at TEXT NOT NULL`, `row_hash TEXT NOT NULL`}
	for _, f := range c.Fields {
		typ := "TEXT"
		if numeric(f) {
			typ = "REAL"
		}
		if f.Type == "boolean" {
			typ = "INTEGER"
		}
		s := quote(f.ID) + " " + typ
		if f.Required {
			s += " NOT NULL"
		}
		if f.Unique {
			s += " UNIQUE"
		}
		cols = append(cols, s)
	}
	_, err := q.ExecContext(ctx, `CREATE TABLE `+table(c)+` (`+strings.Join(cols, ",")+`)`)
	return err
}
func (e *Engine) validateTargets(ctx context.Context, q queryer, c Collection) error {
	for _, f := range c.Fields {
		if f.Type == "relation" && f.Target != c.ID {
			if _, err := getCollection(ctx, q, f.Target, false); err != nil {
				return invalid(f.ID, "target collection does not exist")
			}
		}
	}
	return nil
}
func prepare(p api.CollectionsCreateParams) Collection {
	fields := append([]Field(nil), p.Fields...)
	for i := range fields {
		if fields[i].ID == "" {
			fields[i].ID = newID("f_")
		}
	}
	display := p.DisplayField
	if display == "" && len(fields) > 0 {
		display = fields[0].ID
	}
	return Collection{ID: newID("col_"), Name: p.Name, Icon: p.Icon, Description: p.Description, Fields: fields, DisplayField: display, Summary: p.Summary, CreatedAt: now(), UpdatedAt: now(), Version: 1}
}

// Propose validates without opening a write transaction or persisting anything.
func (e *Engine) Propose(ctx context.Context, p api.CollectionsCreateParams) (Collection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := prepare(p)
	if err := validateSchema(c); err != nil {
		return c, err
	}
	return c, e.validateTargets(ctx, e.db, c)
}
func (e *Engine) Create(ctx context.Context, p api.CollectionsCreateParams) (Collection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := prepare(p)
	err := e.transaction(ctx, func(tx *sql.Tx) error { return e.create(ctx, tx, c) })
	if err == nil {
		e.emit(c.ID, "", "created")
	}
	return c, err
}
func (e *Engine) create(ctx context.Context, q queryer, c Collection) error {
	if err := validateSchema(c); err != nil {
		return err
	}
	if err := e.validateTargets(ctx, q, c); err != nil {
		return err
	}
	if err := createTable(ctx, q, c); err != nil {
		return err
	}
	return saveCollection(ctx, q, c)
}
func (e *Engine) List(ctx context.Context) ([]Collection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return listCollections(ctx, e.db)
}
func (e *Engine) Get(ctx context.Context, id string) (Collection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return getCollection(ctx, e.db, id, false)
}
func (e *Engine) Update(ctx context.Context, p api.CollectionsUpdateParams) (Collection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var c Collection
	err := e.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		c, err = getCollection(ctx, tx, p.ID, false)
		if err != nil {
			return err
		}
		if err = e.snapshot(ctx, tx, c); err != nil {
			return err
		}
		if p.Name != nil {
			c.Name = *p.Name
		}
		if p.Icon != nil {
			c.Icon = *p.Icon
		}
		if p.Description != nil {
			c.Description = *p.Description
		}
		if p.Summary != nil {
			c.Summary = *p.Summary
		}
		if p.DisplayField != nil {
			c.DisplayField = *p.DisplayField
		}
		if err = validateSchema(c); err != nil {
			return err
		}
		c.Version++
		c.UpdatedAt = now()
		return saveCollection(ctx, tx, c)
	})
	if err == nil {
		e.emit(c.ID, "", "updated")
	}
	return c, err
}

type schemaSnapshot struct {
	Collection Collection `json:"collection"`
	Records    []Record   `json:"records"`
}

func (e *Engine) snapshot(ctx context.Context, q queryer, c Collection) error {
	records, err := readRecords(ctx, q, c, "", nil, `id`, 0, 0)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(schemaSnapshot{c, records})
	if err != nil {
		return err
	}
	if _, err = q.ExecContext(ctx, `INSERT OR REPLACE INTO schema_versions VALUES(?,?,?,?)`, c.ID, c.Version, string(raw), now()); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `DELETE FROM schema_versions WHERE collection_id=? AND version NOT IN (SELECT version FROM schema_versions WHERE collection_id=? ORDER BY version DESC LIMIT ?)`, c.ID, c.ID, e.versions)
	return err
}

// Versions returns retained, restorable schema snapshots in newest-first order.
func (e *Engine) Versions(ctx context.Context, id string) ([]int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rows, err := e.db.QueryContext(ctx, `SELECT version FROM schema_versions WHERE collection_id=? ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var v int
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (e *Engine) Fields(ctx context.Context, p api.CollectionsFieldsParams) (api.CollectionsFieldsResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := api.CollectionsFieldsResult{}
	err := e.transaction(ctx, func(tx *sql.Tx) error {
		c, err := getCollection(ctx, tx, p.ID, false)
		if err != nil {
			return err
		}
		old := c
		records, err := readRecords(ctx, tx, c, "", nil, `id`, 0, 0)
		if err != nil {
			return err
		}
		c.Fields = append([]Field(nil), c.Fields...)
		for _, id := range p.Remove {
			if _, err = fieldByID(c, id); err != nil {
				return err
			}
			next := []Field{}
			for _, f := range c.Fields {
				if f.ID != id {
					next = append(next, f)
				}
			}
			c.Fields = next
			for i := range records {
				delete(records[i].Values, id)
			}
			if c.DisplayField == id && len(next) > 0 {
				c.DisplayField = next[0].ID
			}
		}
		for _, r := range p.Rename {
			found := false
			for i := range c.Fields {
				if c.Fields[i].ID == r.Field {
					c.Fields[i].Name = r.Name
					found = true
				}
			}
			if !found {
				return invalid(r.Field, "unknown field")
			}
		}
		for _, f := range p.Add {
			if f.ID == "" {
				f.ID = newID("f_")
			}
			c.Fields = append(c.Fields, f)
		}
		for _, change := range p.ChangeType {
			found := false
			report := api.FieldConversion{Field: change.Field, Failed: []api.ConversionFailure{}}
			for i := range c.Fields {
				if c.Fields[i].ID == change.Field {
					found = true
					c.Fields[i].Type = change.Type
					if change.Options != nil {
						c.Fields[i].Options = change.Options
					}
					for j := range records {
						value := records[j].Values[change.Field]
						if value != nil && (change.Type == "text" || change.Type == "long_text") {
							value = fmt.Sprint(value)
						}
						v, conversionErr := Normalize(c.Fields[i], value)
						if conversionErr != nil {
							report.Failed = append(report.Failed, api.ConversionFailure{RecordID: records[j].ID, Value: records[j].Values[change.Field]})
						} else {
							records[j].Values[change.Field] = v
							report.Converted++
						}
					}
				}
			}
			if !found {
				return invalid(change.Field, "unknown field")
			}
			out.Report = append(out.Report, report)
		}
		if err = validateSchema(c); err != nil {
			return err
		}
		if err = e.validateTargets(ctx, tx, c); err != nil {
			return err
		}
		out.Collection = c
		for _, r := range out.Report {
			if len(r.Failed) > 0 {
				if p.DryRun {
					return nil
				}
				return invalid(r.Field, "conversion failed; run dry_run to inspect records")
			}
		}
		// Rebuilding inside the transaction exercises required/unique/relation checks
		// for every value and rolls back atomically, including during dry runs.
		if err = e.snapshot(ctx, tx, old); err != nil {
			return err
		}
		if len(p.Add) > 0 || len(p.Remove) > 0 || len(p.ChangeType) > 0 {
			if _, err = tx.ExecContext(ctx, `DROP TABLE `+table(old)); err != nil {
				return err
			}
			if err = createTable(ctx, tx, c); err != nil {
				return err
			}
			for i := range records {
				for _, f := range c.Fields {
					v, normalizeErr := Normalize(f, records[i].Values[f.ID])
					if normalizeErr != nil {
						return normalizeErr
					}
					records[i].Values[f.ID] = v
				}
				if err = insertSQL(ctx, tx, c, records[i]); err != nil {
					return invalid("fields", "required or unique constraint failed during conversion")
				}
			}
			for _, r := range records {
				if _, err = validateValues(ctx, tx, c, r.Values, r.ID); err != nil {
					return err
				}
			}
		}
		c.Version++
		c.UpdatedAt = now()
		out.Collection = c
		if err = saveCollection(ctx, tx, c); err != nil {
			return err
		}
		if p.DryRun {
			return errDryRun
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	if err == nil && !p.DryRun {
		e.emit(p.ID, "", "fields")
	}
	return out, err
}

var errDryRun = errors.New("rollback dry run")

func references(ctx context.Context, q queryer, target, id string) ([]api.RelatedRecords, error) {
	all, err := listCollections(ctx, q)
	if err != nil {
		return nil, err
	}
	out := []api.RelatedRecords{}
	for _, c := range all {
		for _, f := range c.Fields {
			if f.Type != "relation" || f.Target != target {
				continue
			}
			whereSQL := ""
			var args []any
			if id != "" {
				whereSQL = quote(f.ID) + "=?"
				args = []any{id}
			} else {
				whereSQL = quote(f.ID) + " IS NOT NULL"
			}
			records, err := readRecords(ctx, q, c, whereSQL, args, `id`, 0, 0)
			if err != nil {
				return nil, err
			}
			if len(records) > 0 {
				out = append(out, api.RelatedRecords{Collection: c.ID, Field: f.ID, Records: records})
			}
		}
	}
	return out, nil
}
func (e *Engine) Delete(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.transaction(ctx, func(tx *sql.Tx) error {
		if _, err := getCollection(ctx, tx, id, false); err != nil {
			return err
		}
		refs, err := references(ctx, tx, id, "")
		if err != nil {
			return err
		}
		for _, r := range refs {
			if r.Collection != id {
				return referenceError(refs)
			}
		}
		// Even empty incoming relation fields must remain valid after deletion.
		all, err := listCollections(ctx, tx)
		if err != nil {
			return err
		}
		for _, c := range all {
			if c.ID == id {
				continue
			}
			for _, f := range c.Fields {
				if f.Type == "relation" && f.Target == id {
					return invalid(f.ID, "remove the referencing relation field in "+c.ID+" before deleting the collection")
				}
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE collection_meta SET deleted_at=? WHERE id=?`, now(), id)
		return err
	})
	if err == nil {
		e.emit(id, "", "deleted")
	}
	return err
}
func referenceError(refs []api.RelatedRecords) error {
	raw, _ := json.Marshal(refs)
	return invalid("relation", "referenced by "+string(raw))
}

// Restore restores a trashed collection, or a retained schema and its record
// snapshot. Schema restoration must be confirmed by the caller.
func (e *Engine) Restore(ctx context.Context, id string, version int) (Collection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var c Collection
	err := e.transaction(ctx, func(tx *sql.Tx) error {
		var err error
		c, err = getCollection(ctx, tx, id, true)
		if err != nil {
			return err
		}
		var deleted sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT deleted_at FROM collection_meta WHERE id=?`, id).Scan(&deleted); err != nil {
			return err
		}
		if deleted.Valid {
			t, _ := time.Parse(time.RFC3339Nano, deleted.String)
			if time.Since(t) > TrashRetention {
				return ErrNotFound
			}
		}
		if version > 0 {
			var raw string
			if err = tx.QueryRowContext(ctx, `SELECT snapshot FROM schema_versions WHERE collection_id=? AND version=?`, id, version).Scan(&raw); err != nil {
				return err
			}
			var snap schemaSnapshot
			if err = json.Unmarshal([]byte(raw), &snap); err != nil {
				return err
			}
			// Replacing IDs would break external relations; block any incoming references.
			refs, err := references(ctx, tx, id, "")
			if err != nil {
				return err
			}
			for _, r := range refs {
				if r.Collection != id {
					return referenceError(refs)
				}
			}
			if err = e.snapshot(ctx, tx, c); err != nil {
				return err
			}
			nextVersion := c.Version + 1
			c = snap.Collection
			c.Version = nextVersion
			c.UpdatedAt = now()
			if err = e.validateTargets(ctx, tx, c); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DROP TABLE `+table(c)); err != nil {
				return err
			}
			if err = createTable(ctx, tx, c); err != nil {
				return err
			}
			// Insert rows first, then validate self-references against the complete table.
			for _, r := range snap.Records {
				if err = insertSQL(ctx, tx, c, r); err != nil {
					return err
				}
			}
			for _, r := range snap.Records {
				if _, err = validateValues(ctx, tx, c, r.Values, r.ID); err != nil {
					return err
				}
			}
			if err = saveCollection(ctx, tx, c); err != nil {
				return err
			}
		}
		if err = e.validateTargets(ctx, tx, c); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE collection_meta SET deleted_at=NULL WHERE id=?`, id)
		return err
	})
	if err == nil {
		e.emit(id, "", "restored")
	}
	return c, err
}
func (e *Engine) Trash(ctx context.Context) ([]Collection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rows, err := e.db.QueryContext(ctx, `SELECT schema_json FROM collection_meta WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Collection{}
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			return nil, err
		}
		var c Collection
		if err = json.Unmarshal([]byte(s), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (e *Engine) purge(ctx context.Context) error {
	return e.transaction(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM collection_meta WHERE deleted_at<?`, time.Now().UTC().Add(-TrashRetention).Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err = tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table(Collection{ID: id})); err != nil {
				return err
			}
			for _, t := range []string{"collection_meta", "collection_fields", "schema_versions"} {
				key := "collection_id"
				if t == "collection_meta" {
					key = "id"
				}
				if _, err = tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE `+key+`=?`, id); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func hashValues(v map[string]any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
