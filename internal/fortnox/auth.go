package fortnox

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	authEndpoint  = "https://apps.fortnox.se/oauth-v1/auth"
	tokenEndpoint = "https://apps.fortnox.se/oauth-v1/token"
)

// Token holds OAuth2 tokens and their expiry.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Valid reports whether the access token is present and not expired.
func (t Token) Valid() bool {
	return t.AccessToken != "" && time.Now().Before(t.ExpiresAt.Add(-30*time.Second))
}

// AuthURL builds the OAuth2 authorization URL the user must visit.
func AuthURL(clientID, redirectURI, scopes, state string) string {
	params := url.Values{
		"client_id":     {clientID},
		"response_type": {"code"},
		"redirect_uri":  {redirectURI},
		"scope":         {scopes},
		"state":         {state},
		"access_type":   {"offline"},
	}
	return authEndpoint + "?" + params.Encode()
}

// ExchangeCode exchanges an authorization code for tokens.
func ExchangeCode(clientID, clientSecret, redirectURI, code string) (Token, error) {
	return postToken(clientID, clientSecret, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	})
}

// RefreshAccessToken uses the refresh token to obtain a new access token.
func RefreshAccessToken(clientID, clientSecret, refreshToken string) (Token, error) {
	return postToken(clientID, clientSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func postToken(clientID, clientSecret string, body url.Values) (Token, error) {
	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, strings.NewReader(body.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("build token request: %w", err)
	}
	creds := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + clientSecret))
	req.Header.Set("Authorization", "Basic "+creds)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Token{}, fmt.Errorf("decode token response: %w", err)
	}
	if raw.Error != "" {
		return Token{}, fmt.Errorf("token error %s: %s", raw.Error, raw.ErrorDesc)
	}
	return Token{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		TokenType:    raw.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second),
	}, nil
}

// SaveToken writes the token to a mode-specific token file. Pass the mode's
// TokenFile() value as path so sandbox and real-readonly tokens never share
// state.
func SaveToken(path string, t Token) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open token file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return json.NewEncoder(f).Encode(t)
}

// LoadToken reads the token from a mode-specific token file.
func LoadToken(path string) (Token, error) {
	f, err := os.Open(path)
	if err != nil {
		return Token{}, fmt.Errorf("open token file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	var t Token
	if err := json.NewDecoder(f).Decode(&t); err != nil {
		return Token{}, fmt.Errorf("decode token file %s: %w", path, err)
	}
	return t, nil
}
