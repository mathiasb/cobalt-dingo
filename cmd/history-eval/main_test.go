package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// main cannot be run in a test (it needs a database and Fortnox), so this reads
// its source. Text from Fortnox, the database or the tenant record must not
// reach a Job log that agent sessions read and a brain wiki ingests (#94). The
// guard is a whitelist over the shapes main.go may use to emit text; anything
// else is a violation, and adding a shape is a decision someone writes down here.
//
// It checks the SHAPE of every emitting call. It does not prove no secret flows
// into a value: values of log attributes are pinned to reviewed expressions, and
// errors are only returned through fixed text or safe().
func violations(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, 0)
	require.NoError(t, err)

	var out []string
	bad := func(n ast.Node, format string, a ...any) {
		out = append(out, fmt.Sprintf("%s: %s", fset.Position(n.Pos()), fmt.Sprintf(format, a...)))
	}

	// Format -> the exact arguments that follow it. The format alone is not
	// enough: the same format around a tenant name leaks just as well.
	errorfAllowed := map[string]string{
		"config: %w":                           "err",                              // config.Load echoes the FORTNOX_MODE the operator set
		"FORTNOX_INTEGRATION_KEY unusable: %w": "err",                              // crypto.NewCipher reports a length, never the key
		"list tenants: %w":                     "err",                              // a plain SQL error; carries no row
		"%s: %s":                               "what, historyeval.SafeError(err)", // safe(): fixed class text only
	}
	sprintfAllowed := map[string]string{"read invoice %d of %d": "i + 1, len(heads)"}
	args := func(es []ast.Expr) string {
		parts := make([]string, len(es))
		for i, e := range es {
			parts[i] = types.ExprString(e)
		}
		return strings.Join(parts, ", ")
	}
	// Attribute key -> the only expressions that may be its value.
	logValues := map[string]map[string]bool{
		"mode":  {"cfg.Mode": true},
		"count": {"len(heads)": true},
		"from":  {"from": true},
		"to":    {"to": true},
		"done":  {"i + 1": true},
		"of":    {"len(heads)": true},
		"err":   {"err": true},
	}
	slogAllowed := map[string]bool{"New": true, "NewTextHandler": true}

	// Where the identifier log may appear: as the receiver of Info/Warn/Error.
	logRecv := map[*ast.Ident]bool{}
	inFunc := func(name string) *ast.FuncDecl {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
				return fd
			}
		}
		return nil
	}
	mainFn, runFn := inFunc("main"), inFunc("run")
	require.NotNil(t, mainFn)
	require.NotNil(t, runFn)

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "panic", "print", "println":
				bad(call, "%s() writes or carries text the guard cannot follow", id.Name)
			case "safe":
				// The label is a literal or the one reviewed Sprintf; the error is err.
				label := types.ExprString(call.Args[0])
				_, lit := stringLit(call.Args[0])
				if !lit && label != `fmt.Sprintf("read invoice %d of %d", i + 1, len(heads))` || len(call.Args) != 2 || types.ExprString(call.Args[1]) != "err" {
					bad(call, "safe(%s) is not a reviewed call", args(call.Args))
				}
			}
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, _ := sel.X.(*ast.Ident)
		name := sel.Sel.Name
		switch {
		case pkg != nil && pkg.Name == "log" && (name == "Info" || name == "Warn" || name == "Error"):
			logRecv[pkg] = true
			if _, ok := stringLit(call.Args[0]); !ok {
				bad(call, "log.%s message is not a literal", name)
			}
			for i := 1; i < len(call.Args); i += 2 {
				key, ok := stringLit(call.Args[i])
				if !ok || logValues[key] == nil {
					bad(call, "log.%s uses the attribute key %q, which is not reviewed", name, key)
					continue
				}
				if i+1 >= len(call.Args) || !logValues[key][types.ExprString(call.Args[i+1])] {
					bad(call, "log.%s attribute %q has a value that is not the reviewed expression", name, key)
				}
			}
		case name == "With" || name == "WithGroup" || name == "WithAttrs":
			bad(call, "%s() derives a logger the guard cannot follow", name)
		case pkg != nil && pkg.Name == "fmt":
			switch name {
			case "Errorf":
				format, ok := stringLit(call.Args[0])
				want, listed := errorfAllowed[format]
				if !ok || !listed || args(call.Args[1:]) != want {
					bad(call, "fmt.Errorf(%q, %s) is not a reviewed call", format, args(call.Args[1:]))
				}
			case "Sprintf":
				format, ok := stringLit(call.Args[0])
				want, listed := sprintfAllowed[format]
				if !ok || !listed || args(call.Args[1:]) != want {
					bad(call, "fmt.Sprintf(%q, %s) is not a reviewed call", format, args(call.Args[1:]))
				}
			case "Print":
				if len(call.Args) != 1 || types.ExprString(call.Args[0]) != "historyeval.Render(historyeval.Analyse(invoices, suppliers, testFraction))" {
					bad(call, "fmt.Print prints something other than the aggregate report")
				}
			case "Println":
				if _, ok := stringLit(call.Args[0]); !ok || len(call.Args) != 1 {
					bad(call, "fmt.Println prints something other than one literal")
				}
			default:
				bad(call, "fmt.%s is not on the reviewed list", name)
			}
		case pkg != nil && pkg.Name == "errors" && name == "Join":
			bad(call, "errors.Join wraps an error the guard cannot follow")
		case pkg != nil && pkg.Name == "errors" && name == "New":
			if _, ok := stringLit(call.Args[0]); !ok {
				bad(call, "errors.New takes a non-literal")
			}
		case pkg != nil && pkg.Name == "slog" && !slogAllowed[name]:
			bad(call, "slog.%s writes through a logger the guard cannot follow", name)
		}
		return true
	})

	// os.Stderr / os.Stdout only where main builds the one logger.
	ast.Inspect(f, func(n ast.Node) bool {
		s, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := s.X.(*ast.Ident); ok && x.Name == "os" && (s.Sel.Name == "Stderr" || s.Sel.Name == "Stdout") {
			if n.Pos() < mainFn.Pos() || n.Pos() > mainFn.End() {
				bad(n, "os.%s used outside main()", s.Sel.Name)
			}
		}
		return true
	})

	// In run(), log may only be a receiver of Info/Warn/Error: not aliased,
	// not passed to a constructor (those log the tenant id, #157 review).
	ast.Inspect(runFn.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "log" && !logRecv[id] {
			bad(id, "log is used other than as the receiver of Info/Warn/Error")
		}
		if r, ok := n.(*ast.ReturnStmt); ok {
			for _, res := range r.Results {
				if id, ok := res.(*ast.Ident); ok && id.Name != "nil" {
					bad(r, "run returns %s as it is, not through safe() or fixed text", id.Name)
				}
			}
		}
		return true
	})
	return out
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func TestMain_OnlyReviewedShapesCanEmitText(t *testing.T) {
	src, err := os.ReadFile("main.go")
	require.NoError(t, err)
	assert.Empty(t, violations(t, string(src)))
}

// Each case rewrites main.go the way a careless edit would and expects the
// guard to object. A case that does not change the source fails, so a stale
// target cannot make the table pass vacuously.
func TestMain_GuardObjectsToTheRouteEachCaseOpens(t *testing.T) {
	src, err := os.ReadFile("main.go")
	require.NoError(t, err)
	base := string(src)

	cases := []struct{ name, from, to string }{
		{"tenant id logged", `log.Info("reading", "mode", cfg.Mode)`, `log.Info("reading", "tenant", tenant.ID)`},
		{"tenant id logged under a reviewed key", `log.Info("reading", "mode", cfg.Mode)`, `log.Info("reading", "mode", tenant.ID)`},
		{"logger aliased", `log.Info("reading", "mode", cfg.Mode)`, `l := log; l.Info("reading", "mode", cfg.Mode)`},
		{"logger derived with With", `log.Info("reading", "mode", cfg.Mode)`, `log.With("mode", cfg.Mode).Info("reading", "mode", cfg.Mode)`},
		{"real logger handed to the token store", `cfg, tokenHeadroom, quiet)`, `cfg, tokenHeadroom, log)`},
		{"real logger handed to the refresh lock", `postgres.NewRefreshLock(store, quiet)`, `postgres.NewRefreshLock(store, log)`},
		{"default logger handed to the refresh lock", `postgres.NewRefreshLock(store, quiet)`, `postgres.NewRefreshLock(store, slog.Default())`},
		{"default slog logger", `log.Info("reading", "mode", cfg.Mode)`, `slog.Info("reading", "tenant", tenant.ID)`},
		{"fmt.Println of a name", `fmt.Println("company confirmed with Fortnox: yes")`, `fmt.Println(tenant.Name)`},
		{"stderr written directly", `fmt.Println("company confirmed with Fortnox: yes")`, `fmt.Fprintln(os.Stderr, tenant.ID)`},
		{"error returned raw", `return safe("load token", err)`, `return err`},
		{"error formatted with %v", `return safe("load token", err)`, `return fmt.Errorf("load token: %v", err)`},
		{"error formatted with %s", `return safe("confirm company", err)`, `return fmt.Errorf("confirm company: %s", err)`},
		{"errors.New with a variable", `return errors.New("DATABASE_URL is required: the stored token lives in postgres")`, `return errors.New(dsn)`},
		{"safe() given a tenant name as its label", `return safe("load token", err)`, `return safe(tenant.Name, err)`},
		{"safe() formats a tenant name instead of the class", `what, historyeval.SafeError(err))`, `what, tenant.Name)`},
		{"allowed wrap format around another value", `fmt.Errorf("list tenants: %w", err)`, `fmt.Errorf("list tenants: %w", errors.New(tenant.Name))`},
		{"invoice counter replaced by a given number", `fmt.Sprintf("read invoice %d of %d", i+1, len(heads))`, `fmt.Sprintf("read invoice %d of %d", h.GivenNumber, len(heads))`},
		{"log message carries the tenant id", `log.Info("reading", "mode", cfg.Mode)`, `log.Info(tenant.ID, "mode", cfg.Mode)`},
		{"panic with the tenant id", `fmt.Println("company confirmed with Fortnox: yes")`, `panic(tenant.ID)`},
		{"println builtin", `fmt.Println("company confirmed with Fortnox: yes")`, `println(tenant.Name)`},
		{"errors.Join around the raw error", `return safe("load token", err)`, `return errors.Join(err)`},
		{"report printed with something extra", `fmt.Print(historyeval.Render(historyeval.Analyse(invoices, suppliers, testFraction)))`, `fmt.Print(historyeval.Render(historyeval.Analyse(invoices, suppliers, testFraction)), tenant.Name)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Contains(t, base, c.from, "the target text moved; update the case")
			mutated := strings.Replace(base, c.from, c.to, 1)
			assert.NotEmpty(t, violations(t, mutated), "the guard let this through")
		})
	}
}
