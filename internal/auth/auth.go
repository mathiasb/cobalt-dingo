// Package auth defines the TokenStore interface and Token type for
// OAuth2 token persistence. Other packages depend on this interface,
// never on a concrete implementation.
package auth

import (
	"errors"
	"time"
)

// ErrNoToken is returned by TokenStore.Load when no token has been persisted yet.
var ErrNoToken = errors.New("auth: no token stored – run OAuth2 flow first")

// Token holds an OAuth2 access/refresh token pair.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	TokenType    string    `json:"token_type"`
}

// IsExpired reports whether the access token should be refreshed.
// A 60-second buffer is applied so callers have time to complete a request.
func (t *Token) IsExpired() bool {
	return time.Now().After(t.Expiry.Add(-60 * time.Second))
}

// TokenStore is the contract for OAuth2 token persistence.
type TokenStore interface {
	Save(token *Token) error
	Load() (*Token, error)
}
