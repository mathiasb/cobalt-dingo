---
name: htmx-patterns
description: HTMX conventions — default attributes, form patterns, validation errors, hypermedia-first API design. Use when writing HTMX templates or Go handlers that return HTML fragments.
---

# HTMX patterns

## Default attributes
Always include on interactive elements:
- `hx-indicator` for loading states
- `hx-swap="innerHTML"` as default (explicit over implicit)
- `hx-target` pointing to a specific ID, never `this` in production

## Form pattern
```html
<form hx-post="/items" hx-target="#item-list" hx-swap="beforeend" hx-indicator="#spinner">
  <input type="text" name="title" required>
  <button type="submit">Add</button>
  <span id="spinner" class="htmx-indicator">...</span>
</form>
```

## Server-sent validation errors
Return 422 with the error fragment, swap into the form's error container:
```html
hx-target-422="#form-errors"
```

## Prefer hypermedia over JSON
If the endpoint returns data for display, return an HTML fragment.
Only use JSON for machine-to-machine APIs or when a non-browser client needs it.
