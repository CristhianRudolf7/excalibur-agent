package workspace

import (
	"encoding/json"
	"strconv"
)

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

func jsonStr(s string) string {
	q, _ := json.Marshal(s)
	_ = strconv.Itoa
	return string(q)
}
