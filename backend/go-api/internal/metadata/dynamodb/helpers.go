// ==============================================================================
// helpers.go — Small internal utilities for the dynamodb package
// ==============================================================================

package dynamodb

import "encoding/json"

// marshalJSON serialises a value to its JSON string representation.
func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// unmarshalJSON deserialises a JSON string into a value of type T.
func unmarshalJSON[T any](s string) *T {
	if s == "" {
		return nil
	}
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil
	}
	return &v
}
