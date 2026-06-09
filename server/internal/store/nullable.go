package store

import (
	"database/sql"
	"encoding/json"
)

// NullString wraps sql.NullString so it marshals to JSON as either the
// inner string ("value") or null, instead of the default
// {"String":"...","Valid":true} object that clients cannot consume.
//
// Use this for every nullable column we expose over the HTTP API. Scan()
// is inherited from the embedded sql.NullString so it drops into existing
// rows.Scan(&dest) calls unchanged.
type NullString struct {
	sql.NullString
}

// MarshalJSON satisfies json.Marshaler.
func (n NullString) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.String)
}

// UnmarshalJSON accepts either a JSON string or null. Allows clients to
// PATCH a nullable field with either form.
func (n *NullString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		n.Valid = false
		n.String = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	n.Valid = true
	n.String = s
	return nil
}

// String returns the inner string, or "" when not valid. Helper for
// places that want a plain string without a Valid check.
func (n NullString) ValueOr(fallback string) string {
	if n.Valid {
		return n.String
	}
	return fallback
}
