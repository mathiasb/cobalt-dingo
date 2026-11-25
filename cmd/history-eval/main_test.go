package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// main cannot be run in a test (it needs a database and Fortnox), so this reads
// its source. Every error it wraps with %w and every field it logs must be on a
// reviewed list: anything else is a new path by which text from Fortnox, the
// database or the tenant record could reach a Job log that agent sessions read
// and a brain wiki ingests (#94). Adding to a list is a decision someone writes
// down here.
func TestMain_ErrorsAndLogsOnlyCarryReviewedFields(t *testing.T) {
	// Errors wrapped with %w, with why each is safe. The configuration ones carry
	// operator input only (the mode, a key length); none carries Fortnox data.
	wrapAllowed := map[string]string{
		"config: %w":                           "config.Load echoes the FORTNOX_MODE the operator set",
		"FORTNOX_INTEGRATION_KEY unusable: %w": "crypto.NewCipher reports a length, never the key",
		"list tenants: %w":                     "a plain SQL error; carries no row",
	}
	// Keys of slog attributes. None is a tenant id (it embeds the organisation
	// number), a company, a supplier, an invoice or an amount.
	logAllowed := map[string]bool{"mode": true, "count": true, "from": true, "to": true, "done": true, "of": true, "err": true}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	require.NoError(t, err)

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, _ := sel.X.(*ast.Ident)
		switch {
		case recv != nil && recv.Name == "fmt" && sel.Sel.Name == "Errorf" && len(call.Args) > 0:
			if format, ok := stringLit(call.Args[0]); ok && strings.Contains(format, "%w") {
				_, reviewed := wrapAllowed[format]
				assert.True(t, reviewed, "fmt.Errorf(%q) wraps an error that is not on the reviewed list", format)
			}
		case recv != nil && recv.Name == "log" && (sel.Sel.Name == "Info" || sel.Sel.Name == "Error" || sel.Sel.Name == "Warn"):
			// args: message, then key, value, key, value...
			for i := 1; i < len(call.Args); i += 2 {
				key, ok := stringLit(call.Args[i])
				assert.True(t, ok && logAllowed[key], "log.%s logs the field %q, which is not on the reviewed list", sel.Sel.Name, key)
			}
		}
		return true
	})
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
