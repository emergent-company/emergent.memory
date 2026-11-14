#!/usr/bin/env python3
"""POC: convert a subset of MkDocs Material pages to Starlight MDX.

Handles:
  * frontmatter (dropped; the Starlight title is supplied per page)
  * `!!! type "Title"` admonitions      -> <Aside type="...">
  * `=== "Label"` content tabs          -> <Tabs>/<TabItem>
  * a duplicated leading H1            -> removed (Starlight renders the title)
  * inline `|` inside a code span in a table row -> escaped (`\\|`)
  * relative `*.md` links               -> Starlight route for the target page
  * mkdocs-snippets (`--8<--`)          -> reported, not converted

Run from `docs-astro/`:

    python3 scripts/port_mkdocs.py [--src ../docs/site]

It rewrites `src/content/docs/`, so it is the single source of the converted
pages — edit the converter, not the generated MDX.
"""
import argparse
import posixpath
import re
from pathlib import Path

HERE = Path(__file__).resolve().parent
PROJECT = HERE.parent                       # docs-astro/
DEFAULT_SRC = PROJECT.parent / "docs" / "site"
DST = PROJECT / "src" / "content" / "docs"
BASE = "/emergent.memory"

# Pages included in the POC. (source path, output path, title)
# The landing page (`index.mdx`) and the API reference (`api-reference.mdx`) are
# hand-written — a bespoke hero and an embedded Scalar reference rather than a
# conversion of the MkDocs pages.
PAGES = [
    ("developer-guide/index.md", "developer-guide.mdx", "Developer Guide"),
    ("developer-guide/mcp-servers.md", "developer-guide/mcp-servers.mdx", "MCP Servers"),
    ("developer-guide/cli-reference.md", "developer-guide/cli-reference.mdx", "CLI Reference"),
    ("developer-guide/schema.md", "developer-guide/schema.mdx", "Schema"),
    ("developer-guide/security-scopes.md", "developer-guide/security-scopes.mdx", "Security Scopes"),
    ("user-guide/index.md", "user-guide.mdx", "User Guide"),
    ("user-guide/getting-started.md", "user-guide/getting-started.mdx", "Getting Started"),
    ("user-guide/knowledge-graph.md", "user-guide/knowledge-graph.mdx", "Knowledge Graph"),
    ("user-guide/agents.md", "user-guide/agents.mdx", "Agents"),
    ("user-guide/branches.md", "user-guide/branches.mdx", "Branches"),
    ("user-guide/datasources.md", "user-guide/datasources.mdx", "Data Sources"),
    ("go-sdk/index.md", "go-sdk.mdx", "Go SDK"),
    ("python-sdk.md", "python-sdk.mdx", "Python SDK"),
    ("cli.md", "cli.mdx", "CLI"),
]

ASIDE = {
    "note": "note", "abstract": "note", "summary": "note", "tldr": "note",
    "info": "note", "todo": "note", "example": "note", "quote": "note",
    "cite": "note", "question": "note", "help": "note", "faq": "note",
    "tip": "tip", "hint": "tip", "important": "tip", "success": "tip",
    "check": "tip", "done": "tip",
    "warning": "caution", "caution": "caution", "attention": "caution",
    "failure": "danger", "fail": "danger", "missing": "danger",
    "danger": "danger", "error": "danger", "bug": "danger",
}

IMPORTS = "import { Aside, Tabs, TabItem } from '@astrojs/starlight/components';"

ADMON = re.compile(r'^!!!\s+([A-Za-z][\w-]*)(?:\s+"([^"]*)")?\s*$')
TAB = re.compile(r'^===\s+"([^"]*)"\s*$')
MD_LINK = re.compile(r'\]\((?P<target>[^)\s#]+\.md)(?P<frag>#[^)]*)?\)')
CODE_SPAN = re.compile(r'`[^`]*`')
FENCE = re.compile(r'^\s*(```|~~~)')
SNIPPET = re.compile(r'--8<--')


# --------------------------------------------------------------------------- #
# link targets
# --------------------------------------------------------------------------- #
def route_for(src_rel: str) -> str:
    """Starlight route for a MkDocs source page (directory-index aware)."""
    if posixpath.basename(src_rel) in ("index.md", "README.md"):
        parent = posixpath.dirname(src_rel)
        return f"{BASE}/" + (f"{parent}/" if parent else "")
    return f"{BASE}/{src_rel[:-3]}/"


def build_link_map(src_root: Path):
    """Every `.md` file in the source tree -> its future route."""
    routes = {}
    for path in src_root.rglob("*.md"):
        rel = path.relative_to(src_root).as_posix()
        routes[rel] = route_for(rel)
    return routes


# --------------------------------------------------------------------------- #
# markdown -> mdx
# --------------------------------------------------------------------------- #
def indent_of(line: str) -> int:
    return len(line) - len(line.lstrip(" "))


def is_blank(line: str) -> bool:
    return not line.strip()


def collect_body(lines, start, parent_indent):
    """Collect a block indented deeper than parent_indent; returns (dedented, i)."""
    body = []
    i = start
    while i < len(lines):
        line = lines[i]
        if is_blank(line):
            body.append("")
            i += 1
            continue
        if indent_of(line) <= parent_indent:
            break
        body.append(line)
        i += 1
    indents = [indent_of(l) for l in body if not is_blank(l)]
    cut = min(indents) if indents else 0
    out = [(l[cut:] if not is_blank(l) else "") for l in body]
    while out and not out[-1].strip():
        out.pop()
    return out, i


def escape_table_pipes(line: str) -> str:
    """MDX splits a table row on every `|`, so an unescaped pipe inside an
    inline code span (`--from-claude <path|->`) truncates the span and leaves
    raw `<` / JSX in the cell. Escape only inside code spans of table rows."""
    if not line.lstrip().startswith("|"):
        return line
    def fix(m):
        # Do not double-escape a pipe the source already escaped (`<auto\|server>`).
        return re.sub(r"(?<!\\)\|", r"\\|", m.group(0))
    return CODE_SPAN.sub(fix, line)


def rewrite_links(line: str, src_rel: str, routes, stats):
    """Point `*.md` links at the Starlight route of the target page."""
    def fix(m):
        target = posixpath.normpath(
            posixpath.join(posixpath.dirname(src_rel), m.group("target"))
        )
        frag = m.group("frag") or ""
        route = routes.get(target)
        if route is None:
            stats["missing"].add(f"{src_rel} -> {m.group('target')}")
            return m.group(0)
        if target not in PORTS:
            stats["out_of_subset"].add(target)
        return f"]({route}{frag})"
    return MD_LINK.sub(fix, line)


def convert(lines, src_rel, routes, stats):
    out = []
    i = 0
    while i < len(lines):
        line = lines[i]
        stripped = line.strip()
        ind = indent_of(line)

        m = ADMON.match(stripped)
        if m:
            kind, title = m.group(1), m.group(2)
            body, i = collect_body(lines, i + 1, ind)
            aside = ASIDE.get(kind.lower(), "note")
            attr = f' title="{title}"' if title else ""
            out.append(f'<Aside type="{aside}"{attr}>')
            out.append("")
            out.extend(convert(body, src_rel, routes, stats))
            out.append("")
            out.append("</Aside>")
            continue

        m = TAB.match(stripped)
        if m:
            tabs = []
            while i < len(lines):
                cur = lines[i]
                tm = TAB.match(cur.strip())
                if not tm or indent_of(cur) != ind:
                    break
                body, i = collect_body(lines, i + 1, ind)
                tabs.append((tm.group(1), body))
            out.append("<Tabs>")
            for label, body in tabs:
                out.append(f'<TabItem label="{label}">')
                out.append("")
                out.extend(convert(body, src_rel, routes, stats))
                out.append("")
                out.append("</TabItem>")
            out.append("</Tabs>")
            continue

        out.append(rewrite_links(escape_table_pipes(line), src_rel, routes, stats))
        i += 1
    return out


def strip_frontmatter(text):
    if text.startswith("---\n"):
        end = text.find("\n---\n", 4)
        if end != -1:
            return text[end + 5:], text[4:end]
    return text, ""


PORTS = {src for src, _dst, _title in PAGES}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", type=Path, default=Path(DEFAULT_SRC))
    args = ap.parse_args()
    src_root = args.src
    routes = build_link_map(src_root)
    stats = {"missing": set(), "out_of_subset": set()}
    DST.mkdir(parents=True, exist_ok=True)

    for src_rel, dst_rel, title in PAGES:
        src = src_root / src_rel
        if not src.exists():
            print(f"MISSING SOURCE {src_rel}")
            continue
        text, _fm = strip_frontmatter(src.read_text())
        if SNIPPET.search(text):
            print(f"SNIPPET (not converted) {src_rel}")
        lines = text.split("\n")

        for idx, l in enumerate(lines[:12]):
            if l.startswith("# "):
                lines = lines[:idx] + lines[idx + 1:]
                break

        body = convert(lines, src_rel, routes, stats)
        out = ["---", f'title: "{title}"', "---", "", IMPORTS, ""] + body
        dest = DST / dst_rel
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text("\n".join(out).rstrip() + "\n")
        print(f"ok {src_rel} -> {dst_rel}")

    if stats["missing"]:
        print("\nBROKEN LINK TARGETS (no such page in docs/site):")
        for item in sorted(stats["missing"]):
            print(f"  {item}")
    if stats["out_of_subset"]:
        print(f"\nLINKED PAGES OUTSIDE THIS POC SUBSET ({len(stats['out_of_subset'])}):")
        for item in sorted(stats["out_of_subset"]):
            print(f"  {item}")


if __name__ == "__main__":
    main()
