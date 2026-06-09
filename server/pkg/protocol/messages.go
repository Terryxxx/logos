// Package protocol defines the WebSocket message envelope shared by the
// Go server and the desktop UI. Keep this file the single source of truth
// for event type strings — the UI imports the same names via a generated
// or hand-curated TypeScript counterpart.
package protocol

import "encoding/json"

// Envelope is what travels over the /ws connection.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
