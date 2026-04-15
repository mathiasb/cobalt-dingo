---
name: go-patterns
description: Go project patterns — endpoint checklist, error handling, HTMX responses, dependency policy. Use when writing Go code, adding endpoints, or reviewing Go PRs.
---

# Go project patterns

## New endpoint checklist
1. Define request/response types in `types.go`
2. Write handler in `handlers.go` using `http.HandlerFunc`
3. Add route in `routes.go`
4. Write table-driven test in `handlers_test.go`
5. Run `task check` before committing

## Error handling pattern
```go
if err != nil {
    return fmt.Errorf("descriptiveOperation: %w", err)
}
```
Never log and return — do one or the other.

## HTMX response pattern
```go
func (h *Handler) ListItems(w http.ResponseWriter, r *http.Request) {
    items, err := h.store.List(r.Context())
    if err != nil {
        http.Error(w, "failed to list items", http.StatusInternalServerError)
        return
    }
    if r.Header.Get("HX-Request") == "true" {
        h.templates.Render(w, "items/_list", items)
        return
    }
    h.templates.Render(w, "items/index", items)
}
```

## Dependency policy
- Prefer stdlib: `net/http`, `encoding/json`, `database/sql`
- Allowed without justification: `testify`, `slog`, `templ`, `sqlc`
- Needs justification in commit message: anything else
