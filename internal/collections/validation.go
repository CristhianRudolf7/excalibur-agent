// Package collections owns user-defined databases independently of the harness store.
package collections

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

type Field = api.CollectionField
type Collection = api.Collection
type Record = api.RecordRow

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

func (e *ValidationError) Error() string  { return "collection validation failed" }
func invalid(field, message string) error { return &ValidationError{[]FieldError{{field, message}}} }

var identifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)
var fieldTypes = []api.FieldType{"text", "long_text", "number", "integer", "money", "date", "datetime", "boolean", "select", "multi_select", "relation", "email", "url", "file"}

func numeric(f Field) bool { return f.Type == "number" || f.Type == "integer" || f.Type == "money" }
func fieldByID(c Collection, id string) (Field, error) {
	for _, f := range c.Fields {
		if f.ID == id {
			return f, nil
		}
	}
	return Field{}, invalid(id, "unknown field")
}
func validateSchema(c Collection) error {
	if strings.TrimSpace(c.Name) == "" {
		return invalid("name", "name is required")
	}
	if len(c.Fields) == 0 || len(c.Fields) > 128 {
		return invalid("fields", "use between 1 and 128 fields")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, f := range c.Fields {
		if !identifier.MatchString(f.ID) || slices.Contains([]string{"id", "created_at", "updated_at", "row_hash"}, strings.ToLower(f.ID)) || ids[strings.ToLower(f.ID)] {
			return invalid(f.ID, "invalid or duplicate field id")
		}
		ids[strings.ToLower(f.ID)] = true
		name := strings.ToLower(strings.TrimSpace(f.Name))
		if name == "" || names[name] {
			return invalid(f.ID, "empty or duplicate field name")
		}
		names[name] = true
		if !slices.Contains(fieldTypes, f.Type) {
			return invalid(f.ID, "unknown field type")
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return invalid(f.ID, "min exceeds max")
		}
		if f.Type == "money" && !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(f.Currency) {
			return invalid(f.ID, "money requires a three-letter currency")
		}
		if f.Type == "relation" && f.Target == "" {
			return invalid(f.ID, "relation requires a target collection")
		}
		if f.Type == "select" || f.Type == "multi_select" {
			opts := map[string]bool{}
			if len(f.Options) == 0 {
				return invalid(f.ID, "select requires options")
			}
			for _, o := range f.Options {
				key := strings.ToLower(o)
				if strings.TrimSpace(o) == "" || opts[key] {
					return invalid(f.ID, "invalid or duplicate select option")
				}
				opts[key] = true
			}
		}
	}
	if _, err := fieldByID(c, c.DisplayField); err != nil {
		return invalid("display_field", "display field must reference a field")
	}
	cards := map[string]bool{}
	for _, s := range c.Summary {
		if s.ID == "" || cards[s.ID] {
			return invalid("summary", "missing or duplicate card id")
		}
		cards[s.ID] = true
		if !slices.Contains([]string{"count", "sum", "avg", "min", "max", "weighted_avg"}, s.Kind) {
			return invalid(s.ID, "invalid summary kind")
		}
		if s.Chart != "" && !slices.Contains([]string{"none", "bar", "line", "pie"}, s.Chart) {
			return invalid(s.ID, "invalid chart")
		}
		if s.Kind != "count" {
			f, e := fieldByID(c, s.Field)
			if e != nil || !numeric(f) {
				return invalid(s.ID, "summary needs numeric field")
			}
		}
		if s.Kind == "weighted_avg" {
			f, e := fieldByID(c, s.WeightField)
			if e != nil || !numeric(f) {
				return invalid(s.ID, "weighted average needs numeric weight field")
			}
		}
		if s.GroupBy != "" {
			if _, e := fieldByID(c, s.GroupBy); e != nil {
				return e
			}
		}
		if _, _, e := where(c, "", s.Filter); e != nil {
			return e
		}
	}
	return nil
}

// Normalize validates and returns the canonical JSON value. Dates supplied by
// record CRUD follow the local dd/mm/yyyy convention; imports additionally flag
// ambiguous slash dates before calling this function.
func Normalize(f Field, v any) (any, error) {
	if v == nil || v == "" {
		if f.Required {
			return nil, invalid(f.ID, "required value missing")
		}
		return nil, nil
	}
	bad := func() (any, error) { return nil, invalid(f.ID, "invalid "+string(f.Type)+" value") }
	switch f.Type {
	case "number", "integer", "money":
		s := strings.TrimSpace(fmt.Sprint(v))
		if strings.Count(s, ",") == 1 && !strings.Contains(s, ".") {
			s = strings.ReplaceAll(s, ",", ".")
		}
		n, e := strconv.ParseFloat(s, 64)
		if e != nil || math.IsInf(n, 0) || math.IsNaN(n) || (f.Type == "integer" && math.Trunc(n) != n) {
			return bad()
		}
		if (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
			return nil, invalid(f.ID, "value out of range")
		}
		return n, nil
	case "boolean":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(v))) {
		case "sí", "si", "true", "1":
			return true, nil
		case "no", "false", "0":
			return false, nil
		}
		return bad()
	case "date":
		s, ok := v.(string)
		if !ok {
			return bad()
		}
		for _, layout := range []string{"2006-01-02", "02/01/2006", "2/1/2006", "02-01-2006", "2-1-2006"} {
			if d, e := time.Parse(layout, strings.TrimSpace(s)); e == nil {
				return d.Format("2006-01-02"), nil
			}
		}
		return bad()
	case "datetime":
		s, ok := v.(string)
		if !ok {
			return bad()
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "02/01/2006 15:04", "2/1/2006 15:04"} {
			if d, e := time.Parse(layout, strings.TrimSpace(s)); e == nil {
				return d.UTC().Format(time.RFC3339), nil
			}
		}
		return bad()
	case "multi_select":
		var list []string
		switch x := v.(type) {
		case []string:
			list = x
		case []any:
			for _, y := range x {
				s, ok := y.(string)
				if !ok {
					return bad()
				}
				list = append(list, s)
			}
		case string:
			if json.Unmarshal([]byte(x), &list) != nil {
				list = strings.Split(x, ",")
			}
		default:
			return bad()
		}
		out := []string{}
		for _, x := range list {
			if !slices.Contains(f.Options, x) {
				return nil, invalid(f.ID, "unknown option")
			}
			if !slices.Contains(out, x) {
				out = append(out, x)
			}
		}
		return out, nil
	default:
		s, ok := v.(string)
		if !ok {
			return bad()
		}
		switch f.Type {
		case "text", "long_text":
		case "select":
			if !slices.Contains(f.Options, s) {
				return nil, invalid(f.ID, "unknown option")
			}
		case "email":
			a, e := mail.ParseAddress(s)
			if e != nil || a.Address != s {
				return bad()
			}
		case "url":
			u, e := url.Parse(s)
			if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return bad()
			}
		case "file":
			b, e := hex.DecodeString(s)
			if e != nil || len(b) != 32 {
				return bad()
			}
			s = strings.ToLower(s)
		case "relation":
			if s == "" {
				return bad()
			}
		default:
			return bad()
		}
		return s, nil
	}
}
