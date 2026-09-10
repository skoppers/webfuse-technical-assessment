// Package event is the server's copy of the session event contract shared
// with the extension (types.ts): the seven event types and which of them
// are key events. It is the one place either list is defined on the server.
package event

// Type is a session event type as sent by the extension.
type Type string

// The seven event types. FormSubmit and SensitiveURL are key events.
const (
	Click        Type = "click"
	Keydown      Type = "keydown"
	Input        Type = "input"
	Scroll       Type = "scroll"
	Navigation   Type = "navigation"
	FormSubmit   Type = "form_submit"
	SensitiveURL Type = "sensitive_url"
)

var all = map[Type]bool{
	Click: true, Keydown: true, Input: true, Scroll: true,
	Navigation: true, FormSubmit: true, SensitiveURL: true,
}

// Valid reports whether t is one of the seven event types.
func Valid(t Type) bool { return all[t] }

// IsKey reports whether t is a key event: one flushed immediately by the
// extension, carried on the overview stream and highlighted in every UI.
func IsKey(t Type) bool { return t == FormSubmit || t == SensitiveURL }

// KeyTypes returns the key event types as strings, for callers that filter
// stored events by type. It is a fresh slice each call.
func KeyTypes() []string { return []string{string(FormSubmit), string(SensitiveURL)} }
