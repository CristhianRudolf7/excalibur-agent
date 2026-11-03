package collections

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

func validateValues(ctx context.Context, q queryer, c Collection, values map[string]any, id string) (map[string]any, error) {
	out := map[string]any{}
	problems := []FieldError{}
	for key := range values {
		if _, err := fieldByID(c, key); err != nil {
			problems = append(problems, FieldError{key, "unknown field"})
		}
	}
	for _, f := range c.Fields {
		v, err := Normalize(f, values[f.ID])
		if err != nil {
			problems = append(problems, err.(*ValidationError).Errors...)
			continue
		}
		out[f.ID] = v
		if v == nil {
			continue
		}
		if f.Unique {
			var exists int
			if err = q.QueryRowContext(ctx, `SELECT count(*) FROM `+table(c)+` WHERE `+quote(f.ID)+`=? AND id<>?`, sqlValue(v), id).Scan(&exists); err != nil {
				return nil, err
			}
			if exists > 0 {
				problems = append(problems, FieldError{f.ID, "duplicate value"})
			}
		}
		if f.Type == "relation" {
			target, err := getCollection(ctx, q, f.Target, false)
			if f.Target == c.ID {
				target = c
				err = nil
			}
			if err != nil {
				problems = append(problems, FieldError{f.ID, "relation target not found"})
				continue
			}
			var exists int
			if err = q.QueryRowContext(ctx, `SELECT count(*) FROM `+table(target)+` WHERE id=?`, v).Scan(&exists); err != nil {
				return nil, err
			}
			if exists == 0 && !(target.ID == c.ID && v == id) {
				problems = append(problems, FieldError{f.ID, "relation record not found"})
			}
		}
	}
	if len(problems) > 0 {
		return nil, &ValidationError{problems}
	}
	return out, nil
}
func sqlValue(v any) any {
	switch x := v.(type) {
	case []string:
		b, _ := json.Marshal(x)
		return string(b)
	case []any:
		b, _ := json.Marshal(x)
		return string(b)
	}
	return v
}
func insertSQL(ctx context.Context, q queryer, c Collection, r Record) error {
	cols := []string{"id", "created_at", "updated_at", "row_hash"}
	args := []any{r.ID, r.CreatedAt, r.UpdatedAt, hashValues(r.Values)}
	marks := []string{"?", "?", "?", "?"}
	updates := []string{`updated_at=excluded.updated_at`, `row_hash=excluded.row_hash`}
	for _, f := range c.Fields {
		cols = append(cols, f.ID)
		args = append(args, sqlValue(r.Values[f.ID]))
		marks = append(marks, "?")
		updates = append(updates, quote(f.ID)+`=excluded.`+quote(f.ID))
	}
	for i := range cols {
		cols[i] = quote(cols[i])
	}
	_, err := q.ExecContext(ctx, `INSERT INTO `+table(c)+` (`+strings.Join(cols, ",")+`) VALUES (`+strings.Join(marks, ",")+`) ON CONFLICT(id) DO UPDATE SET `+strings.Join(updates, ","), args...)
	return err
}
func writeRecord(ctx context.Context, q queryer, c Collection, values map[string]any, id string, old *Record) (Record, error) {
	if id == "" {
		id = newID("rec_")
	}
	out, err := validateValues(ctx, q, c, values, id)
	if err != nil {
		return Record{}, err
	}
	r := Record{ID: id, Values: out, CreatedAt: now(), UpdatedAt: now()}
	if old != nil {
		r.CreatedAt = old.CreatedAt
		r.UpdatedAt = old.UpdatedAt
	}
	return r, insertSQL(ctx, q, c, r)
}
func readRecords(ctx context.Context, q queryer, c Collection, whereSQL string, args []any, order string, limit, offset int) ([]Record, error) {
	cols := []string{`id`, `created_at`, `updated_at`}
	for _, f := range c.Fields {
		cols = append(cols, quote(f.ID))
	}
	query := `SELECT ` + strings.Join(cols, ",") + ` FROM ` + table(c)
	if whereSQL != "" {
		query += ` WHERE ` + whereSQL
	}
	if order != "" {
		query += ` ORDER BY ` + order
	}
	if limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(append([]any(nil), args...), limit, offset)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		values := make([]any, len(cols))
		pointers := make([]any, len(cols))
		for i := range pointers {
			pointers[i] = &values[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			return nil, err
		}
		r := Record{ID: values[0].(string), CreatedAt: values[1].(string), UpdatedAt: values[2].(string), Values: map[string]any{}}
		for i, f := range c.Fields {
			v := values[i+3]
			if v != nil {
				switch f.Type {
				case "boolean":
					v = v.(int64) != 0
				case "multi_select":
					var list []string
					if err = json.Unmarshal([]byte(v.(string)), &list); err != nil {
						return nil, err
					}
					v = list
				case "number", "integer", "money":
					if n, ok := v.(int64); ok {
						v = float64(n)
					}
				}
			}
			r.Values[f.ID] = v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func getRecord(ctx context.Context, q queryer, c Collection, id string) (Record, error) {
	records, err := readRecords(ctx, q, c, `id=?`, []any{id}, "", 1, 0)
	if err != nil {
		return Record{}, err
	}
	if len(records) == 0 {
		return Record{}, ErrNotFound
	}
	return records[0], nil
}
func (e *Engine) Insert(ctx context.Context, p api.RecordsCreateParams) (Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var r Record
	err := e.transaction(ctx, func(tx *sql.Tx) error {
		c, err := getCollection(ctx, tx, p.Collection, false)
		if err != nil {
			return err
		}
		r, err = writeRecord(ctx, tx, c, p.Values, "", nil)
		if err != nil {
			return err
		}
		c.UpdatedAt = now()
		return saveCollection(ctx, tx, c)
	})
	if err == nil {
		e.emit(p.Collection, r.ID, "record_created")
	}
	return r, err
}
func (e *Engine) UpdateRecord(ctx context.Context, p api.RecordsUpdateParams) (Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var r Record
	err := e.transaction(ctx, func(tx *sql.Tx) error {
		c, err := getCollection(ctx, tx, p.Collection, false)
		if err != nil {
			return err
		}
		old, err := getRecord(ctx, tx, c, p.ID)
		if err != nil {
			return err
		}
		for k, v := range p.Values {
			old.Values[k] = v
		}
		old.UpdatedAt = now()
		r, err = writeRecord(ctx, tx, c, old.Values, p.ID, &old)
		if err != nil {
			return err
		}
		c.UpdatedAt = now()
		return saveCollection(ctx, tx, c)
	})
	if err == nil {
		e.emit(p.Collection, r.ID, "record_updated")
	}
	return r, err
}
func (e *Engine) DeleteRecord(ctx context.Context, p api.RecordsIDParams) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	err := e.transaction(ctx, func(tx *sql.Tx) error {
		c, err := getCollection(ctx, tx, p.Collection, false)
		if err != nil {
			return err
		}
		if _, err = getRecord(ctx, tx, c, p.ID); err != nil {
			return err
		}
		refs, err := references(ctx, tx, c.ID, p.ID)
		if err != nil {
			return err
		}
		blocking := []api.RelatedRecords{}
		for _, ref := range refs {
			remaining := []Record{}
			for _, r := range ref.Records {
				if ref.Collection != c.ID || r.ID != p.ID {
					remaining = append(remaining, r)
				}
			}
			if len(remaining) > 0 {
				ref.Records = remaining
				blocking = append(blocking, ref)
			}
		}
		if len(blocking) > 0 {
			return referenceError(blocking)
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table(c)+` WHERE id=?`, p.ID); err != nil {
			return err
		}
		c.UpdatedAt = now()
		return saveCollection(ctx, tx, c)
	})
	if err == nil {
		e.emit(p.Collection, p.ID, "record_deleted")
	}
	return err
}
func (e *Engine) Record(ctx context.Context, p api.RecordsIDParams) (api.RecordsGetResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := api.RecordsGetResult{Related: []api.RelatedRecords{}}
	c, err := getCollection(ctx, e.db, p.Collection, false)
	if err != nil {
		return out, err
	}
	out.Record, err = getRecord(ctx, e.db, c, p.ID)
	if err != nil {
		return out, err
	}
	for _, f := range c.Fields {
		if f.Type == "relation" && out.Record.Values[f.ID] != nil {
			target, err := getCollection(ctx, e.db, f.Target, false)
			if err != nil {
				return out, err
			}
			r, err := getRecord(ctx, e.db, target, fmt.Sprint(out.Record.Values[f.ID]))
			if err != nil {
				return out, err
			}
			out.Related = append(out.Related, api.RelatedRecords{Collection: f.Target, Field: f.ID, Records: []Record{r}})
		}
	}
	refs, err := references(ctx, e.db, c.ID, p.ID)
	out.Related = append(out.Related, refs...)
	return out, err
}
func where(c Collection, query string, filters []api.CollectionFilter) (string, []any, error) {
	clauses := []string{}
	args := []any{}
	if query != "" {
		likes := []string{}
		for _, f := range c.Fields {
			if f.Type == "text" || f.Type == "long_text" || f.Type == "email" {
				likes = append(likes, quote(f.ID)+` LIKE ? ESCAPE '\'`)
				escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
				args = append(args, "%"+escaped+"%")
			}
		}
		if len(likes) == 0 {
			clauses = append(clauses, "0")
		} else {
			clauses = append(clauses, "("+strings.Join(likes, " OR ")+")")
		}
	}
	for _, filter := range filters {
		f, err := fieldByID(c, filter.Field)
		if err != nil {
			return "", nil, err
		}
		col := quote(f.ID)
		switch filter.Op {
		case "is_empty":
			clauses = append(clauses, "("+col+` IS NULL OR `+col+`='' OR `+col+`='[]')`)
		case "not_empty":
			clauses = append(clauses, "("+col+` IS NOT NULL AND `+col+`<>'' AND `+col+`<>'[]')`)
		case "contains":
			clauses = append(clauses, `instr(lower(CAST(`+col+` AS TEXT)),lower(?))>0`)
			args = append(args, fmt.Sprint(filter.Value))
		case "in":
			raw, _ := json.Marshal(filter.Value)
			var values []any
			if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
				return "", nil, invalid(f.ID, "in needs a nonempty array")
			}
			marks := []string{}
			for _, v := range values {
				normalized, e := Normalize(Field{ID: f.ID, Type: f.Type, Options: f.Options}, v)
				if e != nil {
					return "", nil, e
				}
				marks = append(marks, "?")
				args = append(args, sqlValue(normalized))
			}
			clauses = append(clauses, col+` IN (`+strings.Join(marks, ",")+`)`)
		default:
			op, ok := map[string]string{"eq": "=", "neq": "<>", "lt": "<", "lte": "<=", "gt": ">", "gte": ">="}[filter.Op]
			if !ok {
				return "", nil, invalid(f.ID, "unknown filter operator")
			}
			if filter.Value == nil {
				if filter.Op == "eq" {
					clauses = append(clauses, col+" IS NULL")
					continue
				}
				if filter.Op == "neq" {
					clauses = append(clauses, col+" IS NOT NULL")
					continue
				}
				return "", nil, invalid(f.ID, "comparison requires value")
			}
			normalized, err := Normalize(Field{ID: f.ID, Type: f.Type, Options: f.Options}, filter.Value)
			if err != nil {
				return "", nil, err
			}
			clauses = append(clauses, col+op+"?")
			args = append(args, sqlValue(normalized))
		}
	}
	return strings.Join(clauses, " AND "), args, nil
}

type pageCursor struct {
	Offset    int    `json:"offset"`
	Signature string `json:"signature"`
}

func listRecords(ctx context.Context, q queryer, p api.RecordsListParams) (api.RecordsListResult, error) {
	out := api.RecordsListResult{Records: []Record{}}
	c, err := getCollection(ctx, q, p.Collection, false)
	if err != nil {
		return out, err
	}
	w, args, err := where(c, p.Query, p.Filters)
	if err != nil {
		return out, err
	}
	order := []string{}
	for _, s := range p.Sort {
		if _, err = fieldByID(c, s.Field); err != nil {
			return out, err
		}
		dir := strings.ToUpper(s.Dir)
		if dir != "ASC" && dir != "DESC" {
			return out, invalid(s.Field, "sort direction must be asc or desc")
		}
		order = append(order, quote(s.Field)+" "+dir)
	}
	order = append(order, `id ASC`)
	count := `SELECT count(*) FROM ` + table(c)
	if w != "" {
		count += ` WHERE ` + w
	}
	if err = q.QueryRowContext(ctx, count, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	limit := p.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return out, invalid("limit", "limit must be between 1 and 1000")
	}
	signature := hashValues(map[string]any{"collection": p.Collection, "version": c.Version, "query": p.Query, "filters": p.Filters, "sort": p.Sort})
	offset := 0
	if p.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(p.Cursor)
		var cursor pageCursor
		if e != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Offset < 0 || cursor.Signature != signature {
			return out, invalid("cursor", "invalid cursor for this query")
		}
		offset = cursor.Offset
	}
	out.Records, err = readRecords(ctx, q, c, w, args, strings.Join(order, ","), limit, offset)
	if err != nil {
		return out, err
	}
	if offset+len(out.Records) < out.Total {
		raw, _ := json.Marshal(pageCursor{offset + len(out.Records), signature})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
func (e *Engine) Query(ctx context.Context, p api.RecordsListParams) (api.RecordsListResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return listRecords(ctx, e.db, p)
}
func (e *Engine) Summary(ctx context.Context, id string) ([]api.SummaryValue, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := getCollection(ctx, e.db, id, false)
	if err != nil {
		return nil, err
	}
	out := []api.SummaryValue{}
	for _, card := range c.Summary {
		expression := "count(*)"
		switch card.Kind {
		case "sum", "avg", "min", "max":
			expression = card.Kind + "(" + quote(card.Field) + ")"
		case "weighted_avg":
			expression = "sum(" + quote(card.Field) + "*" + quote(card.WeightField) + ")/nullif(sum(CASE WHEN " + quote(card.Field) + " IS NOT NULL THEN " + quote(card.WeightField) + " END),0)"
		}
		w, args, err := where(c, "", card.Filter)
		if err != nil {
			return nil, err
		}
		from := ` FROM ` + table(c)
		if w != "" {
			from += ` WHERE ` + w
		}
		v := api.SummaryValue{ID: card.ID, Title: card.Title}
		if err = e.db.QueryRowContext(ctx, `SELECT `+expression+from, args...).Scan(&v.Value); err != nil {
			return nil, err
		}
		if f, err := fieldByID(c, card.Field); err == nil && f.Type == "money" {
			v.Unit = f.Currency
		}
		if card.GroupBy != "" {
			v.Series = []api.SummarySeries{}
			rows, err := e.db.QueryContext(ctx, `SELECT coalesce(CAST(`+quote(card.GroupBy)+` AS TEXT),''),`+expression+from+` GROUP BY `+quote(card.GroupBy)+` ORDER BY `+quote(card.GroupBy), args...)
			if err != nil {
				return nil, err
			}
			for rows.Next() {
				var s api.SummarySeries
				if err = rows.Scan(&s.Label, &s.Value); err != nil {
					rows.Close()
					return nil, err
				}
				v.Series = append(v.Series, s)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
		out = append(out, v)
	}
	return out, nil
}
