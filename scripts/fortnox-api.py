#!/usr/bin/env python3
"""Look up a Fortnox resource in the vendored OpenAPI spec.

Why a lookup tool rather than a RAG. The spec is structured, so an exact query
answers exactly; BM25 over the same content answers approximately and can be
confidently wrong. This session produced five Fortnox mistakes and the two that
survived reading the prose docs were both field-shape questions the HTML
documentation simply does not contain — /3/inbox being one. Those are answerable
from the spec and only from the spec.

A RAG is still the right tool for the *guides* — scopes, licensing, the service
account model — which are prose and genuinely unstructured. Different content,
different retrieval.

Usage:
    scripts/fortnox-api.py paths inbox        # which paths match
    scripts/fortnox-api.py show /3/inbox      # methods, params, response fields
    scripts/fortnox-api.py schema Supplier    # a named schema's fields
"""

import json
import os
import sys

SPEC = os.path.join(os.path.dirname(__file__), "..", "docs", "vendor", "fortnox-openapi.json")


def load():
    with open(SPEC) as f:
        return json.load(f)


def resolve(schemas, node, depth=0):
    """Expand a schema node to plain {field: type}, following $ref once."""
    if depth > 3 or not isinstance(node, dict):
        return {}
    if "$ref" in node:
        node = schemas.get(node["$ref"].split("/")[-1], {})
    if node.get("type") == "array":
        return {"[]": resolve(schemas, node.get("items", {}), depth + 1)}
    out = {}
    for name, prop in (node.get("properties") or {}).items():
        if "$ref" in prop or prop.get("type") in ("object", "array"):
            out[name] = resolve(schemas, prop, depth + 1)
        else:
            out[name] = prop.get("type", "?")
    return out


def render(fields, indent=4):
    for name, value in fields.items():
        if isinstance(value, dict):
            print(" " * indent + f"{name}:")
            render(value, indent + 3)
        else:
            print(" " * indent + f"{name}: {value}")


def cmd_paths(spec, term):
    hits = [p for p in spec["paths"] if term.lower() in p.lower()]
    if not hits:
        # Fall back to tags, since a resource is often named differently from
        # its path (a miss here used to mean "not in the API", which is wrong).
        for path, item in spec["paths"].items():
            for op in item.values():
                if isinstance(op, dict) and any(term.lower() in t.lower() for t in op.get("tags", [])):
                    hits.append(path)
        hits = sorted(set(hits))
    for p in hits:
        methods = ",".join(m.upper() for m in spec["paths"][p] if m != "parameters")
        print(f"{p:50s} {methods}")
    if not hits:
        print(f"no path or tag matching {term!r}", file=sys.stderr)
        sys.exit(1)


def cmd_show(spec, path):
    item = spec["paths"].get(path)
    if item is None:
        print(f"{path!r} is not in the spec — try: paths {path.strip('/').split('/')[-1]}", file=sys.stderr)
        sys.exit(1)
    schemas = spec.get("components", {}).get("schemas", {})
    print(path)
    for method, op in item.items():
        if not isinstance(op, dict) or method == "parameters":
            continue
        print(f"\n  {method.upper()} — {op.get('summary', '')}")
        for prm in op.get("parameters", []):
            req = "required" if prm.get("required") else "optional"
            print(f"    param {prm.get('name')} ({prm.get('in')}, {req})")
        for code, resp in sorted(op.get("responses", {}).items()):
            if not code.startswith("2"):
                continue  # errors are the same envelope everywhere
            for media in (resp.get("content") or {}).values():
                fields = resolve(schemas, media.get("schema", {}))
                if fields:
                    print(f"    {code}:")
                    render(fields)


def cmd_schema(spec, name):
    schemas = spec.get("components", {}).get("schemas", {})
    hits = [k for k in schemas if name.lower() in k.lower()]
    if not hits:
        print(f"no schema matching {name!r}", file=sys.stderr)
        sys.exit(1)
    for k in sorted(hits)[:10]:
        print(k)
        render(resolve(schemas, schemas[k]))
        print()


def main():
    if len(sys.argv) < 3:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    spec = load()
    cmd, arg = sys.argv[1], sys.argv[2]
    {"paths": cmd_paths, "show": cmd_show, "schema": cmd_schema}.get(cmd, lambda *_: sys.exit(2))(spec, arg)


if __name__ == "__main__":
    main()
