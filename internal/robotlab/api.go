package robotlab

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/google/jsonschema-go/jsonschema"
)

func invalid(message string) error { return &api.Error{Code: api.CodeInvalidParams, Message: message} }

type methodContract struct {
	Params map[string]any `json:"params"`
	Result map[string]any `json:"result"`
}

var contract struct {
	Methods map[string]methodContract `json:"methods"`
	Defs    map[string]any            `json:"$defs"`
}
var schemaOnce sync.Once
var schemaErr error
var validators map[string]*jsonschema.Resolved

func loadSchemas() {
	schemaErr = json.Unmarshal([]byte(contractJSON), &contract)
	if schemaErr != nil {
		return
	}
	validators = make(map[string]*jsonschema.Resolved, len(contract.Methods))
	for name, method := range contract.Methods {
		schema := schemaWithDefs(method.Params)
		raw, err := json.Marshal(schema)
		if err != nil {
			schemaErr = err
			return
		}
		var parsed jsonschema.Schema
		if err = json.Unmarshal(raw, &parsed); err != nil {
			schemaErr = err
			return
		}
		validators[name], err = parsed.Resolve(nil)
		if err != nil {
			schemaErr = fmt.Errorf("%s: %w", name, err)
			return
		}
	}
}
func schemaWithDefs(schema map[string]any) map[string]any {
	result := make(map[string]any, len(schema)+1)
	for k, v := range schema {
		result[k] = v
	}
	result["$defs"] = contract.Defs
	return result
}
func decodeParams(method string, raw json.RawMessage) (map[string]any, error) {
	schemaOnce.Do(loadSchemas)
	if schemaErr != nil {
		return nil, mapError(schemaErr)
	}
	validator := validators[method]
	if validator == nil {
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown Robot Lab method"}
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	limit := 8 << 20
	if method == "robotlab.media.upload" {
		limit = 45 << 20
	}
	if len(raw) > limit {
		return nil, invalid("parameters exceed size limit")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, invalid(err.Error())
	}
	if err := validator.Validate(value); err != nil {
		return nil, invalid(err.Error())
	}
	params, ok := value.(map[string]any)
	if !ok {
		return nil, invalid("parameters must be an object")
	}
	for key, v := range params {
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			switch key {
			case "spec", "code", "reason":
			default:
				return nil, invalid(key + " must not be blank")
			}
		}
	}
	return params, nil
}
