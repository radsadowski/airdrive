package auth

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// keyringService is the macOS Keychain "service" name every AirDrive
// credential item is filed under; the account name is the location ID,
// so each connected Google Drive location gets its own Keychain item.
const keyringService = "AirDrive"

// SaveToken persists a token in the macOS Keychain, keyed by locationID.
// Zero credentials ever touch SQLite (see CLAUDE.md / plan: "secrets live in
// Keychain, referenced by location_id").
func SaveToken(locationID string, tok GdriveToken) error {
	b, err := json.Marshal(tok) //nolint:gosec // deliberate: the marshaled token is written to the macOS Keychain, never logged
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	if err := keyring.Set(keyringService, locationID, string(b)); err != nil {
		return fmt.Errorf("keychain set: %w", err)
	}
	return nil
}

// LoadToken reads back a previously saved token for locationID.
func LoadToken(locationID string) (GdriveToken, error) {
	s, err := keyring.Get(keyringService, locationID)
	if err != nil {
		return GdriveToken{}, fmt.Errorf("keychain get: %w", err)
	}
	var tok GdriveToken
	if err := json.Unmarshal([]byte(s), &tok); err != nil {
		return GdriveToken{}, fmt.Errorf("unmarshal token: %w", err)
	}
	return tok, nil
}

// DeleteToken removes a location's credentials from the Keychain.
// Safe to call even if nothing is stored (e.g. RemoveLocation on a location
// that failed mid-sign-in) — ErrNotFound is treated as success.
func DeleteToken(locationID string) error {
	if err := keyring.Delete(keyringService, locationID); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("keychain delete: %w", err)
	}
	return nil
}

// SaveCredentials persists opaque per-location credentials in the Keychain.
func SaveCredentials(locationID string, data []byte) error {
	if err := keyring.Set(keyringService, locationID, string(data)); err != nil {
		return fmt.Errorf("keychain set: %w", err)
	}
	return nil
}

// LoadCredentials reads back previously saved credentials.
func LoadCredentials(locationID string) ([]byte, error) {
	s, err := keyring.Get(keyringService, locationID)
	if err != nil {
		return nil, fmt.Errorf("keychain get: %w", err)
	}
	return []byte(s), nil
}

// DeleteCredentials removes a location's credentials; ErrNotFound is success.
func DeleteCredentials(locationID string) error {
	if err := keyring.Delete(keyringService, locationID); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("keychain delete: %w", err)
	}
	return nil
}
