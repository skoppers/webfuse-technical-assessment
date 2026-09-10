// Package jsonx holds small JSON helpers shared across packages.
package jsonx

import "encoding/json"

var emptyObject = json.RawMessage(`{}`)

// OrEmptyObject returns raw, or "{}" when raw is empty, so absent JSON
// values become an empty object rather than null.
func OrEmptyObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return emptyObject
	}
	return raw
}
