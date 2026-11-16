#!/usr/bin/env python3
"""POC: convert a subset of MkDocs Material pages to Starlight MDX.

Handles:
  * frontmatter (dropped; the Starlight title is supplied per page)
  * `!!! type "Title"` admonitions      -> <Aside type="...">
  * `=== "Label"` content tabs          -> <Tabs>/<TabItem>
  * `<div class="grid cards" markdown>` -> <CardGrid>/<Card> (icon from the
    `:material-*:` shortcode on the item's link)
  * remaining `:material-<name>:` shortcodes -> inline <Icon name="lucide:..."/>
    (see `material-icons.json`; an unmapped name is reported, not guessed)
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
import json
import posixpath
import re
from pathlib import Path

HERE = Path(__file__).resolve().parent
PROJECT = HERE.parent                       # docs-astro/
DEFAULT_SRC = PROJECT.parent / "docs" / "site"
DST = PROJECT / "src" / "content" / "docs"
BASE = ""  # site is served at the host root: docs.memory.emergent-company.ai/

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
    ("developer-guide/a2a-protocol.md", "developer-guide/a2a-protocol.mdx", "A2A and A2UI"),
    ("developer-guide/cli-auth.md", "developer-guide/cli-auth.mdx", "CLI Authentication"),
    ("developer-guide/email-setup.md", "developer-guide/email-setup.mdx", "Email Setup"),
    ("developer-guide/extraction.md", "developer-guide/extraction.mdx", "Extraction Pipeline"),
    ("developer-guide/health-ops.md", "developer-guide/health-ops.mdx", "Health & Ops"),
    ("developer-guide/provider-setup.md", "developer-guide/provider-setup.mdx", "Provider Setup"),
    ("developer-guide/deploy-portal.md", "developer-guide/deploy-portal.mdx", "Deploy Portal"),
    ("developer-guide/scheduler.md", "developer-guide/scheduler.mdx", "Background Scheduler"),
    ("developer-guide/github-issue-coding.md", "developer-guide/github-issue-coding.mdx", "GitHub Issue Coding"),
    ("developer-guide/service-accounts.md", "developer-guide/service-accounts.mdx", "Service Accounts"),
    ("developer-guide/sandbox.md", "developer-guide/sandbox.mdx", "Sandbox"),
    ("developer-guide/mcp-relay.md", "developer-guide/mcp-relay.mdx", "MCP Relay"),
    ("user-guide/index.md", "user-guide.mdx", "User Guide"),
    ("user-guide/knowledge-graph.md", "user-guide/knowledge-graph.mdx", "Knowledge Graph"),
    ("user-guide/agents.md", "user-guide/agents.mdx", "Agents"),
    ("user-guide/branches.md", "user-guide/branches.mdx", "Branches"),
    ("user-guide/datasources.md", "user-guide/datasources.mdx", "Data Sources"),
    ("user-guide/api-tokens.md", "user-guide/api-tokens.mdx", "API Tokens"),
    ("user-guide/backups.md", "user-guide/backups.mdx", "Backups"),
    ("user-guide/blueprints.md", "user-guide/blueprints.mdx", "Blueprints"),
    ("user-guide/chat.md", "user-guide/chat.mdx", "Chat"),
    ("user-guide/documents.md", "user-guide/documents.mdx", "Documents"),
    ("user-guide/integrations.md", "user-guide/integrations.mdx", "Integrations"),
    ("user-guide/model-setup.md", "user-guide/model-setup.mdx", "Model Setup"),
    ("user-guide/notifications.md", "user-guide/notifications.mdx", "Notifications"),
    ("user-guide/tasks.md", "user-guide/tasks.mdx", "Tasks"),
    ("tutorials/deploy-to-railway.md", "tutorials/deploy-to-railway.mdx", "Deploy on Railway"),
    ("go-sdk/index.md", "go-sdk.mdx", "Go SDK"),
    ("go-sdk/authentication.md", "go-sdk/authentication.mdx", "Authentication"),
    ("go-sdk/changelog.md", "go-sdk/changelog.mdx", "Changelog"),
    ("go-sdk/error-handling.md", "go-sdk/error-handling.mdx", "Error Handling"),
    ("go-sdk/graph-id-model.md", "go-sdk/graph-id-model.mdx", "Graph ID Model"),
    ("go-sdk/multi-tenancy.md", "go-sdk/multi-tenancy.mdx", "Multi-Tenancy"),
    ("go-sdk/streaming.md", "go-sdk/streaming.mdx", "Streaming"),
    ("python-sdk.md", "python-sdk.mdx", "Python SDK"),
    ("swift-sdk.md", "swift-sdk.mdx", "Swift SDK"),
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
CARD_IMPORTS = (
    "import Card from '../../components/Card.astro';\n"
    "import CardGrid from '../../components/CardGrid.astro';"
)
ICON_IMPORT = "import { Icon } from 'astro-icon/components';"


def component_import(dst_rel, name):
    """Import a local component from a generated page of any depth.

    Pages sit at `src/content/docs/<dirs>/<page>.mdx` and components at
    `src/components/`, so the hop count grows with the page's directory depth —
    a nested page needs one more `../` than a top-level one.
    """
    dirs = posixpath.dirname(dst_rel)
    depth = len(dirs.split('/')) if dirs else 0
    return f"import {name} from '{'../' * (2 + depth)}components/{name}.astro';"

ADMON = re.compile(r'^!!!\s+([A-Za-z][\w-]*)(?:\s+"([^"]*)")?\s*$')
TAB = re.compile(r'^===\s+"([^"]*)"\s*$')
AUTOLINK = re.compile(r'<(?P<url>https?://[^>\s]+)>')
MD_LINK = re.compile(r'\]\((?P<target>[^)\s#]+\.md)(?P<frag>#[^)]*)?\)')
CODE_SPAN = re.compile(r'`[^`]*`')
# Same span, but with a group so `re.split` keeps the code spans in the result
# (`re.split` drops the separators when the pattern has no group — that silently
# deleted every inline code span in the converted pages).
CODE_SPAN_SPLIT = re.compile(r'(`[^`]*`)')
FENCE = re.compile(r'^\s*(```|~~~)')
SNIPPET = re.compile(r'--8<--')

# Material grid cards: `<div class="grid cards" markdown>` wrapping a bullet
# list whose items are `- :material-icon: **[Title](page.md)**` plus an indented
# description. Two pages use them; both styles below appear in the source.
GRID_OPEN = re.compile(r'^<div\s+class="grid cards"\s+markdown>\s*$')
GRID_CLOSE = re.compile(r'^</div>\s*$')
CARD_ITEM = re.compile(r'^-\s+(?::material-(?P<icon>[a-z0-9-]+):\s*)?(?P<head>\S.*?)\s*$')
CARD_LINK = re.compile(r'^\*\*\[(?P<text>[^\]]+)\]\((?P<href>[^)]+)\)\*\*$')
SHORTCODE = re.compile(r':material-([a-z0-9-]+):')
IMAGE = re.compile(r'^!\[(?P<alt>[^\]]*)\]\((?P<src>[^)\s]+)\)\s*$')

# Screenshot dimensions, written by `scripts/port-images.mjs`. Keyed by file
# name; a page's `![alt](path)` becomes a <Screenshot> with a real
# width/height so the page does not jump while images load.
SHOTS = {}
_shot_manifest = PROJECT / 'public' / 'assets' / 'tutorials' / 'railway' / 'manifest.json'
if _shot_manifest.exists():
    SHOTS = json.loads(_shot_manifest.read_text())

# `:material-<name>:` -> lucide icon, shared with the site's `Card` component.
# Keys starting with `_` are prose (`_comment`), not mappings.
ICONS = {
    k: v
    for k, v in json.loads((HERE / "material-icons.json").read_text()).items()
    if not k.startswith("_")
}


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
    while out and not out[0].strip():
        out.pop(0)
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


def resolve_target(target: str, src_rel: str, routes, stats) -> str | None:
    """Route for a link target relative to the page being converted."""
    absolute = posixpath.normpath(posixpath.join(posixpath.dirname(src_rel), target))
    route = routes.get(absolute)
    if route is None:
        stats["missing"].add(f"{src_rel} -> {target}")
        return None
    if absolute not in PORTS:
        stats["out_of_subset"].add(absolute)
    return route


def rewrite_links(line: str, src_rel: str, routes, stats):
    """Point `*.md` links at the Starlight route of the target page."""
    def fix(m):
        target = m.group("target")
        # MD_LINK keys on the `.md` suffix, so an external link to somebody
        # else's `CHANGELOG.md` matches too. It is not a page here and has no
        # route: leave it alone rather than reporting it as a broken target.
        if "://" in target:
            return m.group(0)
        route = resolve_target(target, src_rel, routes, stats)
        if route is None:
            return m.group(0)
        return f"]({route}{m.group('frag') or ''})"
    # MDX parses `<https://…>` as the start of a JSX tag and fails the build, so
    # the Markdown autolink shortcut has to become an explicit link.
    line = AUTOLINK.sub(lambda m: f"[{m.group('url')}]({m.group('url')})", line)
    return MD_LINK.sub(fix, line)


def icon_for(material_name: str, src_rel: str, stats) -> str | None:
    """Iconify name (`lucide:<name>`) for a `:material-<name>:` shortcode."""
    lucide = ICONS.get(material_name)
    if lucide is None:
        stats["unmapped_icons"].add(f":material-{material_name}: ({src_rel})")
        return None
    return f"lucide:{lucide}"


def replace_shortcodes(line: str, src_rel: str, stats) -> str:
    """Inline `:material-name:` outside code spans -> <Icon name="lucide:..."/>."""
    def fix(segment):
        def one(m):
            name = icon_for(m.group(1), src_rel, stats)
            return f'<Icon name="{name}" />' if name else ""
        return SHORTCODE.sub(one, segment)
    parts = CODE_SPAN_SPLIT.split(line)
    # A capturing split alternates text / code span, so only the even indexes
    # (the text around the spans) are rewritten.
    return "".join(fix(p) if i % 2 == 0 else p for i, p in enumerate(parts))


def convert_grid_cards(lines, start, src_rel, routes, stats):
    """`<div class="grid cards" markdown>` -> <CardGrid> of <Card>s.

    `start` indexes the opening `<div>`; returns (mdx_lines, next_index) with
    `next_index` just past the closing `</div>`.
    """
    items = []
    i = start + 1
    while i < len(lines) and not GRID_CLOSE.match(lines[i].strip()):
        line = lines[i]
        if not is_blank(line):
            m = CARD_ITEM.match(line)
            if m is None:
                stats["unparsed_cards"].add(f"{src_rel}: {line.strip()[:70]}")
                i += 1
                continue
            head = m.group("head")
            icon = m.group("icon")
            body, i = collect_body(lines, i + 1, indent_of(line))
            items.append((icon, head, body))
            continue
        i += 1

    out = ["<CardGrid>"]
    for icon, head, body in items:
        link = CARD_LINK.match(head)
        if link:
            route = resolve_target(link.group("href"), src_rel, routes, stats)
            text = link.group("text").replace("'", "&#39;")
            title = f'<a href="{route}">{text}</a>' if route else text
        else:
            title = head.replace("'", "&#39;")
        attrs = f"title='{title}'"
        icon_name = icon_for(icon, src_rel, stats) if icon else None
        if icon_name:
            attrs += f' icon="{icon_name}"'
        out.append(f"<Card {attrs}>")
        out.append("")
        out.extend(convert(body, src_rel, routes, stats))
        out.append("")
        out.append("</Card>")
    out.append("</CardGrid>")
    return out, i + 1


def image_component(alt, src, src_rel, stats):
    """`![alt](path)` -> `<Screenshot>`, sized from the ported-image manifest.

    MkDocs resolves an image path relative to the page that references it, so
    that is the rule here too: one source page has to render the same picture on
    both sites. A page in `tutorials/` therefore writes
    `../assets/tutorials/railway/01.webp`, and this joins it against the page's
    own directory before turning it into a site-root URL.
    """
    if src.startswith("/"):
        rel = posixpath.normpath(src)
    else:
        rel = posixpath.normpath(posixpath.join(posixpath.dirname(src_rel), src))
    name = posixpath.basename(rel)
    dims = SHOTS.get(name)
    if dims is None:
        # Source pages may still point at the .png the guide shipped with; the
        # ported tree carries the same picture as .webp.
        webp = posixpath.splitext(name)[0] + ".webp"
        if webp in SHOTS:
            name, dims = webp, SHOTS[webp]
    if dims is None:
        stats["missing_shots"].add(src)
        return f'<img src="{BASE}/{rel}" alt="{alt}" loading="lazy" />'
    url = f"{BASE}/{posixpath.dirname(rel)}/{name}"
    # `cssWidth`/`cssHeight` are the size the app drew on screen; `width`/`height`
    # are the captured pixels, which can be several times larger. A figure must
    # reserve its on-screen size, never the capture's, or a 4x capture takes four
    # times the space it should. The component reads the captured size from the
    # loaded image when it caps the zoom.
    css_w = dims.get("cssWidth", dims["width"])
    css_h = dims.get("cssHeight", dims["height"])
    return (
        f'<Screenshot src="{url}" alt="{alt}" '
        f'width={{{css_w}}} height={{{css_h}}} />'
    )


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

        if GRID_OPEN.match(stripped):
            cards, i = convert_grid_cards(lines, i, src_rel, routes, stats)
            out.extend(cards)
            continue

        m = IMAGE.match(stripped)
        if m:
            out.append(
                image_component(m.group("alt"), m.group("src"), src_rel, stats)
            )
            i += 1
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


def replace_shortcodes_document(lines, src_rel, stats):
    """Shortcode pass over a whole page, skipping fenced code blocks."""
    out, in_fence = [], False
    for line in lines:
        if FENCE.match(line):
            in_fence = not in_fence
            out.append(line)
            continue
        out.append(line if in_fence else replace_shortcodes(line, src_rel, stats))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", type=Path, default=Path(DEFAULT_SRC))
    args = ap.parse_args()
    src_root = args.src
    routes = build_link_map(src_root)
    stats = {
        "missing": set(),
        "out_of_subset": set(),
        "unmapped_icons": set(),
        "unparsed_cards": set(),
        "missing_shots": set(),
    }
    DST.mkdir(parents=True, exist_ok=True)

    # The generated per-package Go SDK reference is ported wholesale rather than
    # listed in PAGES: the SDK guides link into it (Authentication → auth,
    # Streaming → chat, …), so a partial port turns those into 404s, and the
    # pages are one per package with only an H1 and Go doc bodies. The generator
    # writes the package name as the H1.
    pages = list(PAGES)
    for ref in sorted((src_root / "go-sdk" / "reference").glob("*.md")):
        first = next((l for l in ref.read_text().splitlines() if l.startswith("# ")), "# " + ref.stem)
        pages.append((f"go-sdk/reference/{ref.name}", f"go-sdk/reference/{ref.stem}.mdx", first[2:].strip()))
    pages.append(("developer-guide/schema-registry.md", "developer-guide/schema-registry.mdx", "Schema Registry"))
    # The link checker classifies a target as "outside the subset" by this set,
    # so it has to see the pages appended above, not just the literal PAGES.
    PORTS.clear()
    PORTS.update(src for src, _dst, _title in pages)

    for src_rel, dst_rel, title in pages:
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

        body = replace_shortcodes_document(convert(lines, src_rel, routes, stats), src_rel, stats)
        imports = [IMPORTS]
        if any(line.lstrip().startswith("<Card ") for line in body):
            imports.append(component_import(dst_rel, "Card"))
            imports.append(component_import(dst_rel, "CardGrid"))
        if any("<Icon " in line for line in body):
            imports.append(ICON_IMPORT)
        if any("<Screenshot " in line for line in body):
            imports.append(component_import(dst_rel, "Screenshot"))
        out = ["---", f'title: "{title}"', "---", "", "\n".join(imports), ""] + body
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
    if stats["unmapped_icons"]:
        print("\nUNMAPPED :material-*: SHORTCODES (dropped — add to material-icons.json):")
        for item in sorted(stats["unmapped_icons"]):
            print(f"  {item}")
    if stats["unparsed_cards"]:
        print("\nUNPARSED GRID-CARD LINES (kept as markdown — check the block):")
        for item in sorted(stats["unparsed_cards"]):
            print(f"  {item}")


if __name__ == "__main__":
    main()
