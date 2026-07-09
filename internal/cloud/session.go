package cloud

import (
	"errors"
	"sync"
)

// Session holds the in-memory access token and the (rotating) refresh token and
// transparently refreshes on a 401. The refresh token is never written to disk
// by this type; onRotate lets the caller persist it (e.g. to the OS keychain).
type Session struct {
	Client   *Client
	mu       sync.Mutex
	access   string
	refresh  string
	onRotate func(Tokens)
}

// NewSession builds a Session. onRotate may be nil.
func NewSession(c *Client, access, refresh string, onRotate func(Tokens)) *Session {
	return &Session{Client: c, access: access, refresh: refresh, onRotate: onRotate}
}

// Access returns the current access token.
func (s *Session) Access() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access
}

// WithAccess runs fn with the current access token. If fn returns
// ErrUnauthorized, it refreshes the token pair (invoking onRotate with the new
// tokens) and retries fn exactly once. Any other error is returned as-is.
func (s *Session) WithAccess(fn func(access string) error) error {
	s.mu.Lock()
	access := s.access
	s.mu.Unlock()

	err := fn(access)
	if !errors.Is(err, ErrUnauthorized) {
		return err
	}

	s.mu.Lock()
	refresh := s.refresh
	s.mu.Unlock()

	tok, rerr := s.Client.Refresh(refresh)
	if rerr != nil {
		return rerr
	}

	s.mu.Lock()
	s.access = tok.Access
	s.refresh = tok.Refresh
	s.mu.Unlock()
	if s.onRotate != nil {
		s.onRotate(tok)
	}
	return fn(tok.Access)
}
