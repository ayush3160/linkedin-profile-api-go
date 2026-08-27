package linkedin

import (
	"encoding/json"
	"strconv"
)

// intOf reads a numeric field, tolerating both json.Number and string forms
// (SDUI serialises some int64 fields as strings).
func intOf(obj *Object, key string) int {
	value, ok := obj.Get(key)
	if !ok {
		return 0
	}
	switch n := value.(type) {
	case json.Number:
		parsed, err := n.Int64()
		if err != nil {
			return 0
		}
		return int(parsed)
	case string:
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return 0
		}
		return parsed
	case float64:
		return int(n)
	}
	return 0
}
