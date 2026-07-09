package cloud

import (
	"sync"

	keyring "github.com/zalando/go-keyring"
)

// CredStore persists the long-lived refresh token. The access token stays in
// process memory only and is never given to a CredStore.
type CredStore interface {
	SaveRefresh(token string) error
	LoadRefresh() (string, error)
	DeleteRefresh() error
}

// KeyringStore stores the refresh token in the OS credential store (Windows
// Credential Manager). A missing entry reads back as an empty string with no
// error, so callers can treat "no token" and "signed out" uniformly.
type KeyringStore struct {
	Service string
	User    string
}

// SaveRefresh writes token to the OS keychain.
func (k KeyringStore) SaveRefresh(token string) error {
	return keyring.Set(k.Service, k.User, token)
}

// LoadRefresh reads the token; a missing entry yields ("", nil).
func (k KeyringStore) LoadRefresh() (string, error) {
	v, err := keyring.Get(k.Service, k.User)
	if err == keyring.ErrNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// DeleteRefresh removes the token; a missing entry is not an error.
func (k KeyringStore) DeleteRefresh() error {
	err := keyring.Delete(k.Service, k.User)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

// MemCredStore is an in-memory CredStore for tests and headless use.
type MemCredStore struct {
	mu    sync.Mutex
	token string
}

// SaveRefresh stores token in memory.
func (m *MemCredStore) SaveRefresh(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = token
	return nil
}

// LoadRefresh returns the in-memory token ("" when unset).
func (m *MemCredStore) LoadRefresh() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token, nil
}

// DeleteRefresh clears the in-memory token.
func (m *MemCredStore) DeleteRefresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = ""
	return nil
}
