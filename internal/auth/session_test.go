package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mathiasb/cobalt-dingo/internal/auth"
)

// Clear must carry the same protective flags as Set. A deletion cookie without
// them is a second, weaker Set-Cookie for the same name — which is what #14 was
// filed about. The fix landed on main via an agent run that triggered no CI, so
// this test is the first thing to actually check it.
func TestClearCookieCarriesProtectiveFlags(t *testing.T) {
	m := auth.NewSessionManager("test-secret")
	w := httptest.NewRecorder()

	m.Clear(w)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1, "Clear must write exactly one cookie")

	got := cookies[0]

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"HttpOnly", got.HttpOnly, true},
		{"Secure", got.Secure, true},
		{"SameSite", got.SameSite, http.SameSiteLaxMode},
		{"MaxAge", got.MaxAge, -1},
		{"Value", got.Value, ""},
		{"Path", got.Path, "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.got)
		})
	}
}
