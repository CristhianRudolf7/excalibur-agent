package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/collections"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

// CollectionRoutes owns the daemon-only collections store and blob adapter.
type CollectionRoutes struct {
	Engine *collections.Engine
	Blobs  gateway.BlobStore
}

func NewCollectionRoutes(dataDir string, blobs gateway.BlobStore, bus *telemetry.Bus) (*CollectionRoutes, error) {
	engine, err := collections.Open(dataDir, func(event api.CollectionsChangedEvent) {
		if bus != nil {
			_, _ = bus.Publish(api.EventCollectionsChanged, nil, nil, event)
		}
	})
	if err != nil {
		return nil, err
	}
	return &CollectionRoutes{Engine: engine, Blobs: blobs}, nil
}
func (s *CollectionRoutes) Close() error {
	if s == nil {
		return nil
	}
	return s.Engine.Close()
}
func decodeCollections(raw json.RawMessage, p any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return &api.Error{Code: api.CodeInvalidParams, Message: "expected object params"}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(p); err != nil {
		return &api.Error{Code: api.CodeInvalidParams, Message: "invalid collection parameters: " + err.Error()}
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return &api.Error{Code: api.CodeInvalidParams, Message: "expected one params object"}
	}
	value := reflect.ValueOf(p).Elem()
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		tag := field.Tag.Get("json")
		if strings.Contains(tag, ",omitempty") {
			continue
		}
		v := value.Field(i)
		missing := false
		switch v.Kind() {
		case reflect.String:
			missing = strings.TrimSpace(v.String()) == ""
		case reflect.Map, reflect.Slice:
			missing = v.IsNil()
		}
		if missing {
			return &api.Error{Code: api.CodeInvalidParams, Message: tag + " is required"}
		}
	}

	return nil
}
func collectionError(err error) error {
	if err == nil {
		return nil
	}
	var apiError *api.Error
	if errors.As(err, &apiError) {
		return apiError
	}
	var validation *collections.ValidationError
	if errors.As(err, &validation) {
		return &api.Error{Code: api.CodeInvalidParams, Message: "collection validation failed", Data: validation}
	}
	if errors.Is(err, collections.ErrNotFound) {
		return &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
	}
	return &api.Error{Code: api.CodeInternal, Message: err.Error()}
}
func (s *CollectionRoutes) Op(ctx context.Context, method string, raw json.RawMessage) (result any, err error) {
	defer func() { err = collectionError(err) }()
	if s == nil || s.Engine == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "collections store unavailable"}
	}
	switch method {
	case api.MethodCollectionsList:
		var p struct{}
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		list, err := s.Engine.List(ctx)
		return api.CollectionsListResult{Collections: list}, err
	case api.MethodCollectionsGet:
		var p api.CollectionsIDParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		c, err := s.Engine.Get(ctx, p.ID)
		return api.CollectionResult{Collection: c}, err
	case api.MethodCollectionsCreate:
		var p api.CollectionsCreateParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		c, err := s.Engine.Create(ctx, p)
		return api.CollectionResult{Collection: c}, err
	case api.MethodCollectionsUpdate:
		var p api.CollectionsUpdateParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		c, err := s.Engine.Update(ctx, p)
		return api.CollectionResult{Collection: c}, err
	case api.MethodCollectionsFields:
		var p api.CollectionsFieldsParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		return s.Engine.Fields(ctx, p)
	case api.MethodCollectionsDelete:
		var p api.CollectionsIDParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		err = s.Engine.Delete(ctx, p.ID)
		return map[string]bool{"ok": err == nil}, err
	case api.MethodRecordsList:
		var p api.RecordsListParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		return s.Engine.Query(ctx, p)
	case api.MethodRecordsGet:
		var p api.RecordsIDParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		return s.Engine.Record(ctx, p)
	case api.MethodRecordsCreate:
		var p api.RecordsCreateParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		r, err := s.Engine.Insert(ctx, p)
		return api.RecordResult{Record: r}, err
	case api.MethodRecordsUpdate:
		var p api.RecordsUpdateParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		r, err := s.Engine.UpdateRecord(ctx, p)
		return api.RecordResult{Record: r}, err
	case api.MethodRecordsDelete:
		var p api.RecordsIDParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		err = s.Engine.DeleteRecord(ctx, p)
		return map[string]bool{"ok": err == nil}, err
	case api.MethodCollectionsSummary:
		var p api.CollectionsIDParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		cards, err := s.Engine.Summary(ctx, p.ID)
		return api.CollectionsSummaryResult{Cards: cards}, err
	case api.MethodCollectionsImport:
		var p api.CollectionsImportParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		return s.Engine.Import(ctx, s.Blobs, p)
	case api.MethodCollectionsExport:
		var p api.CollectionsExportParams
		if err = decodeCollections(raw, &p); err != nil {
			return nil, err
		}
		return s.Engine.Export(ctx, s.Blobs, p)
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown collections method"}
	}
}
