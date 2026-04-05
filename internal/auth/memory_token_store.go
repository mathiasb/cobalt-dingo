package auth

// MemoryTokenStore is an in-memory TokenStore for use in tests.
// It stores the token as-is without encryption.
type MemoryTokenStore struct {
	token *Token
}

// Save stores the token in memory, replacing any previous token.
func (m *MemoryTokenStore) Save(token *Token) error {
	cp := *token
	m.token = &cp
	return nil
}

// Load returns the stored token, or ErrNoToken if none has been saved.
func (m *MemoryTokenStore) Load() (*Token, error) {
	if m.token == nil {
		return nil, ErrNoToken
	}
	cp := *m.token
	return &cp, nil
}
