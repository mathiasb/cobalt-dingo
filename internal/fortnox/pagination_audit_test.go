package fortnox

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// singlePageByDesign registers every collection read that deliberately does
// NOT paginate, with the reason. Adding a method here is a decision someone
// has to write down; leaving it out fails this test.
//
// Why a registry and not a convention: pagination has been missed three times
// in this client — the voucher list returned exactly 100 of 405 and produced a
// confident wrong answer about account 1930 (#90), ListAccounts had the same
// gap, and both unpaid-invoice fetches read page one only (#91). Each time the
// helper already existed and the call site simply did not use it. A rule
// nobody can forget beats a rule everybody knows.
var singlePageByDesign = map[string]string{
	"GetAllPages": "it IS the pagination helper, so paginating it would recurse forever",
	"TotalResources": "reads @TotalResources from page one on purpose — the count is the " +
		"whole answer, and paginating a counter would cost exactly what the count exists to avoid",

	// Counters built on TotalResources. They issue one request per filter and
	// decode no records at all, so there is nothing to truncate.
	"invoiceStateCounts": "one TotalResources call per status filter; returns counts, " +
		"not records, so there is no page of data to miss",
	"SupplierInvoiceStateCounts": "delegates to invoiceStateCounts — counts, not records",
	"CustomerInvoiceStateCounts": "delegates to invoiceStateCounts — counts, not records",

	// Bounded by something other than how much data the company has.
	"ListPredefinedAccounts": "the system-defined account roles are a fixed Fortnox set of " +
		"roughly thirty entries; it cannot grow with the company's data",
	"ListFinancialYears": "one record per financial year the company has existed, so page " +
		"one covers its first century of trading",
	"ListSupplierInvoicePayments": "payments recorded against ONE supplier invoice; more " +
		"than a hundred part-payments of a single invoice is not a real case",
	"ListCustomerInvoicePayments": "payments received against ONE customer invoice; same " +
		"reasoning as the supplier side",

	// Sandbox seeding and teardown, in internal/fortnox/seed.go. These run
	// against a test company this suite created and populated itself, so the
	// collection size is set by the seed rather than by real data. A truncated
	// page here leaves test residue behind; it cannot produce a wrong financial
	// figure, because none of these is reachable from a production read path.
	"ListSuppliers":                  "sandbox seed tooling; bounded by what the suite itself seeded",
	"ListCustomers":                  "sandbox seed tooling; bounded by what the suite itself seeded",
	"ListProjectsByPrefix":           "sandbox seed tooling; bounded by what the suite itself seeded",
	"ListAssetsByPrefix":             "sandbox seed tooling; bounded by what the suite itself seeded",
	"ListCostCentersByPrefix":        "sandbox seed tooling; bounded by what the suite itself seeded",
	"ListSupplierInvoicesBySupplier": "sandbox teardown; truncation leaves test residue, never a wrong figure",
	"ListCustomerInvoicesByCustomer": "sandbox teardown; truncation leaves test residue, never a wrong figure",
}

// Collection reads must paginate, or say why not.
//
// Fortnox paginates list endpoints at 100 records and reports @TotalPages in
// every response, so a single-page read of a larger collection returns a
// truncated set with nothing about it to notice. That is worse than an error:
// it is plausible.
func TestEveryCollectionReadPaginatesOrIsRegistered(t *testing.T) {
	methods := parseClientMethods(t)

	var unregistered []string
	for name, m := range methods {
		if !m.returnsCollection {
			continue
		}
		if _, ok := singlePageByDesign[name]; ok {
			continue
		}
		if paginates(name, methods, map[string]bool{}) {
			continue
		}
		unregistered = append(unregistered, name)
	}

	require.Empty(t, unregistered,
		"these methods return a collection without paginating and without a registered reason.\n"+
			"Either route them through GetAllPages, or add them to singlePageByDesign with why:\n  %s",
		strings.Join(unregistered, "\n  "))
}

// The registry must not rot: an entry naming a method that no longer exists
// hides the fact that nobody has looked at this in a while.
func TestSinglePageRegistryHasNoStaleEntries(t *testing.T) {
	methods := parseClientMethods(t)
	for name := range singlePageByDesign {
		require.Contains(t, methods, name, "singlePageByDesign names a method that does not exist")
	}
}

// Every registered exemption needs a real reason, not a placeholder.
func TestSinglePageRegistryReasonsAreSubstantive(t *testing.T) {
	for name, reason := range singlePageByDesign {
		require.Greater(t, len(reason), 30, "reason for %s is too short to be a reason", name)
	}
}

type clientMethod struct {
	returnsCollection bool
	callsGetAllPages  bool
	callsMethods      []string
}

// parseClientMethods reads this package's source and describes every method on
// *Client. Source analysis rather than reflection: whether a method paginates
// is a property of its body, which reflection cannot see.
func parseClientMethods(t *testing.T) map[string]clientMethod {
	t.Helper()

	// ParseFile over a glob rather than ParseDir, which staticcheck flags as
	// deprecated for ignoring build tags. This package has none, and the
	// alternative it recommends would add a dependency to run one test.
	sources, err := filepath.Glob("*.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	methods := map[string]clientMethod{}
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)

		{
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				d := describe(fn)
				// Only *Client methods are AUDITED, but every function is
				// described, so the call graph can follow a package-level
				// helper like listAll.
				if !isClientMethod(fn) {
					d.returnsCollection = false
				}
				methods[fn.Name.Name] = d
			}
		}
	}
	require.NotEmpty(t, methods, "no *Client methods found — the audit would pass vacuously")
	return methods
}

func isClientMethod(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "Client"
}

func describe(fn *ast.FuncDecl) clientMethod {
	var m clientMethod

	if fn.Type.Results != nil {
		for _, r := range fn.Type.Results.List {
			if _, isSlice := r.Type.(*ast.ArrayType); isSlice {
				m.returnsCollection = true
			}
			// A map result is a collection too — the invoice state counters
			// return one, and they reach the API once per key.
			if _, isMap := r.Type.(*ast.MapType); isMap {
				m.returnsCollection = true
			}
		}
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Both call shapes matter. `c.GetAllPages(...)` is a selector;
		// `listAll(c, ...)` — the generic page loop — is a plain identifier,
		// and following only selectors missed every method routed through it.
		var callee string
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			callee = fun.Sel.Name
		case *ast.Ident:
			callee = fun.Name
		default:
			return true
		}
		if callee == "GetAllPages" {
			m.callsGetAllPages = true
		}
		m.callsMethods = append(m.callsMethods, callee)
		return true
	})
	return m
}

// paginates reports whether a method reaches GetAllPages, directly or through
// another method on the same client. Delegation is the normal shape here —
// UnpaidSupplierInvoices goes through supplierInvoicesByFilter — so a direct
// check would demand duplication.
func paginates(name string, methods map[string]clientMethod, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true

	m, ok := methods[name]
	if !ok {
		return false
	}
	if m.callsGetAllPages {
		return true
	}
	for _, callee := range m.callsMethods {
		if paginates(callee, methods, seen) {
			return true
		}
	}
	return false
}
