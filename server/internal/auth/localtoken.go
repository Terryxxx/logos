// Package auth manages the single localhost token used to authenticate
// the desktop UI against the embedded server. In V0.1 the threat model is
// simply "other local processes on this machine"; we don't try to defend
// against an attacker who has filesystem read on the user's data dir.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/logos-app/logos/server/internal/store"
)

const settingKey = "localhost_token"

// LoadOrCreateToken returns the persisted token, creating one on first run.
func LoadOrCreateToken(st *store.Store) (string, error) {
	tok, err := st.GetSetting(settingKey)
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	if tok != "" {
		return tok, nil
	}
	tok, err = generate()
	if err != nil {
		return "", err
	}
	if err := st.SetSetting(settingKey, tok); err != nil {
		return "", fmt.Errorf("persist token: %w", err)
	}
	return tok, nil
}

func generate() (string, error) {
	b := make([]byte, 32) // 256 bits
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "logos_" + hex.EncodeToString(b), nil
}
