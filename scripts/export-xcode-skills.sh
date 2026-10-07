#!/usr/bin/env bash
#
# Export the agent skills that ship inside Xcode (27+) and install them into an
# opencode/Claude-style skills directory.
#
# Xcode and opencode both use the same "Agent Skills" format: one directory per
# skill containing a SKILL.md (YAML frontmatter + markdown) and an optional
# references/ directory. The only transformation needed is stripping the
# ".packaged" suffix Xcode adds to bundled resources.
#
# The Xcode toolchain lives on the macOS build host (default ssh alias
# `mcj-mini`); the export runs there and is rsync'd back.
#
# The skills are Apple-licensed (Xcode SLA §2.5/§2.7: no copying-in-part, no
# hosting/redistribution), so the default install dir is the local opencode
# skills directory — never the repo. Point --dest at a Git tree at your own risk;
# the generated skill dirs are gitignored as a guard.
#
# Usage:
#   scripts/export-xcode-skills.sh                 # export + install into the local opencode skills dir
#   scripts/export-xcode-skills.sh --dry-run       # export + validate, but do not write to dest
#   scripts/export-xcode-skills.sh --check         # validate existing dest only (no ssh)
#   scripts/export-xcode-skills.sh --host HOST --dest DIR --remote-dir DIR
#
set -euo pipefail

HOST="mcj-mini"
REMOTE_DEVELOPER_DIR="/Applications/Xcode.app/Contents/Developer"
REMOTE_DIR=""              # resolved to <remote-home>/xcode-27-skills when empty
DEST=""                    # resolved to the local opencode skills dir when empty
DRY_RUN=0
CHECK=0

# The Xcode 27 skill directories this exporter manages. --check validates exactly
# these (not every skill the user happens to have installed locally).
XCODE_SKILLS=(
    adopt-c-bounds-safety
    app-intents-specialist
    app-intents-whats-new-27
    audit-xcode-security-settings
    building-document-based-swiftui-applications
    device-interaction
    modernize-tests
    swiftui-specialist
    swiftui-whats-new-27
    uikit-app-modernization
)

usage() {
    sed -n '/^# Usage:/,/^#$/p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
    case "$1" in
        --host)       HOST="$2"; shift 2 ;;
        --dest)       DEST="$2"; shift 2 ;;
        --remote-dir) REMOTE_DIR="$2"; shift 2 ;;
        --dry-run)    DRY_RUN=1; shift ;;
        --check)      CHECK=1; shift ;;
        -h|--help)    usage; exit 0 ;;
        *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

# Default install dir: the machine-local opencode skills directory. Never the
# repo — this content is Apple-licensed and must not be committed.
DEST="${DEST:-${XDG_CONFIG_HOME:-$HOME/.config}/opencode/skills}"

log()  { printf '  %s\n' "$*"; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
die()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Validation
# ---------------------------------------------------------------------------

# Extract the YAML frontmatter (between the first two '---' lines) of a SKILL.md.
frontmatter() {
    awk 'NR==1 && $0=="---"{f=1;next} f && $0=="---"{exit} f{print}' "$1"
}

# Extract a scalar field value. Handles plain, quoted, and |/> block scalars.
fm_field() {
    local fm="$1" key="$2" raw
    raw="$(printf '%s\n' "$fm" | sed -n "s/^${key}:[[:space:]]*//p" | head -n1)"
    case "$raw" in
        '|'|'|-'|'|+'|'>'|'>-'|'>+')
            raw="$(printf '%s\n' "$fm" | awk -v k="$key" '
                $0 ~ ("^" k ":") { f=1; next }
                f { if ($0 ~ /^[ \t]/ || $0 == "") { sub(/^[ \t]+/, ""); printf "%s ", $0 } else exit }
            ')"
            ;;
    esac
    printf '%s' "$raw" \
        | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//" \
        | tr -s '[:space:]' ' ' \
        | sed -e 's/^ //' -e 's/ $//'
}

validate_skill() {
    local root="$1" name="$2"
    local d="$root/$name" skill="$root/$name/SKILL.md"
    local rc=0

    [ -f "$skill" ] || { warn "'$name': no SKILL.md under $root"; return 1; }

    # name must match opencode's required regex.
    if ! [[ "$name" =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]] || [ "${#name}" -gt 64 ]; then
        warn "'$name': invalid skill name (must be lowercase alnum + single hyphens, <=64)"; rc=1
    fi

    local fm fname fdesc dlen
    fm="$(frontmatter "$skill")"
    fname="$(fm_field "$fm" name)"
    fdesc="$(fm_field "$fm" description)"

    [ -n "$fname" ] || { warn "'$name': missing frontmatter 'name'"; rc=1; }
    [ -n "$fdesc" ] || { warn "'$name': missing frontmatter 'description'"; rc=1; }
    [ -z "$fname" ] || [ "$fname" = "$name" ] || { warn "'$name': frontmatter name '$fname' != directory name"; rc=1; }

    dlen=${#fdesc}
    if [ "$dlen" -gt 1024 ]; then
        warn "'$name': description is $dlen chars (max 1024)"
        rc=1
    fi

    if [ -d "$d/references" ]; then
        local missing=0
        # Every references/*.md referenced from SKILL.md must exist.
        while IFS= read -r ref; do
            [ -f "$d/$ref" ] || { warn "'$name': SKILL.md references missing '$ref'"; rc=1; missing=1; }
        done < <(grep -oE 'references/[A-Za-z0-9._/-]+\.md' "$skill" 2>/dev/null | sort -u || true)
        [ "$missing" -eq 0 ] && log "'$name': $dlen-char description, references/ present"
    else
        log "'$name': $dlen-char description"
    fi

    return "$rc"
}

validate_dir() {
    local root="$1"
    local rc=0 count=0

    shopt -s nullglob
    for d in "$root"/*/; do
        local name; name="$(basename "$d")"
        [ -f "$d/SKILL.md" ] || { warn "no SKILL.md in $name — skipping"; continue; }
        count=$((count + 1))
        validate_skill "$root" "$name" || rc=1
    done
    shopt -u nullglob

    [ "$count" -gt 0 ] || { warn "no skill directories found under $root"; rc=1; }
    return "$rc"
}

# ---------------------------------------------------------------------------
# Check-only mode
# ---------------------------------------------------------------------------
if [ "$CHECK" -eq 1 ]; then
    echo "Validating installed Xcode 27 skills in $DEST"
    if [ ! -d "$DEST" ]; then
        die "no skills installed at $DEST — run 'task skills:export-xcode' first, or pass --dest DIR"
    fi
    rc=0 found=0
    for name in "${XCODE_SKILLS[@]}"; do
        if [ -f "$DEST/$name/SKILL.md" ]; then
            found=$((found + 1))
            validate_skill "$DEST" "$name" || rc=1
        else
            warn "'$name': not installed at $DEST/$name"; rc=1
        fi
    done
    [ "$rc" -eq 0 ] || die "validation failed"
    echo "OK ($found/${#XCODE_SKILLS[@]} skills)"
    exit 0
fi

# ---------------------------------------------------------------------------
# Export from the macOS build host
# ---------------------------------------------------------------------------
command -v ssh   >/dev/null || die "ssh not found"
command -v rsync >/dev/null || die "rsync not found"

REMOTE_HOME="$(ssh "$HOST" 'printf %s "$HOME"')" || die "cannot ssh to $HOST"
REMOTE_DIR="${REMOTE_DIR:-$REMOTE_HOME/xcode-27-skills}"

echo "Exporting Xcode skills from $HOST:$REMOTE_DIR"
# shellcheck disable=SC2029  # local paths intentionally interpolate into the remote command
ssh "$HOST" "export DEVELOPER_DIR='$REMOTE_DEVELOPER_DIR'; xcrun agent skills export --output-dir '$REMOTE_DIR' --replace-existing" \
    || die "xcrun agent skills export failed on $HOST"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "Fetching skills"
rsync -az --delete "$HOST:$REMOTE_DIR/" "$STAGE/"

# Strip the ".packaged" suffix Xcode uses for bundled resources.
find "$STAGE" -type f -name '*.packaged' -print0 | while IFS= read -r -d '' f; do
    mv "$f" "${f%.packaged}"
done

echo "Normalizing frontmatter for opencode"
command -v python3 >/dev/null || die "python3 not found"
python3 - "$STAGE" <<'PY'
import os, re, sys

root, MAX = sys.argv[1], 1024

def split_fm(text):
    m = re.match(r'^\ufeff?---[ \t]*\n(.*?)\n---[ \t]*(?:\n|$)(.*)$', text, re.S)
    return (m.group(1), m.group(2)) if m else (None, text)

def parse_scalars(fm):
    """Top-level key -> value, supporting | and > block scalars."""
    lines, out, i = fm.split('\n'), {}, 0
    while i < len(lines):
        m = re.match(r'^([A-Za-z0-9_-]+):[ \t]*(.*)$', lines[i])
        if not m:
            i += 1
            continue
        key, rest = m.group(1), m.group(2).rstrip()
        if rest[:1] in ('|', '>'):
            i += 1
            block = []
            while i < len(lines) and (lines[i][:1] in (' ', '\t') or lines[i] == ''):
                block.append(lines[i].strip())
                i += 1
            while block and block[-1] == '':
                block.pop()
            out[key] = (' ' if rest[0] == '>' else '\n').join(block)
        else:
            val = rest
            if len(val) >= 2 and val[0] == val[-1] and val[0] in ('"', "'"):
                val = val[1:-1]
            out[key] = val
            i += 1
    return out

collapse = lambda s: re.sub(r'\s+', ' ', s).strip()

def truncate(s, limit):
    if len(s) <= limit:
        return s
    cut, best = s[:limit], -1
    for sep in ('; ', '. ', ' - ', ', ', ' \u2014 ', ' '):
        idx = cut.rfind(sep)
        if idx > best:
            best = idx + (0 if sep == ' ' else len(sep))
    if best < limit // 2:
        best = limit
    return s[:best].rstrip()

for name in sorted(os.listdir(root)):
    d = os.path.join(root, name)
    skill = os.path.join(d, 'SKILL.md')
    if not os.path.isdir(d) or not os.path.isfile(skill):
        continue
    fm, body = split_fm(open(skill, encoding='utf-8').read())
    if fm is None:
        print(f'WARN: {name}: no frontmatter, leaving as-is')
        continue
    fields = parse_scalars(fm)
    nm = (fields.get('name') or name).strip()
    desc = collapse(fields.get('description', ''))
    when = collapse(fields.get('when_to_use', ''))
    combined = desc if (not when or when in desc) else collapse(desc + ' ' + when)
    if len(combined) > MAX:
        combined = truncate(combined, MAX)
    indent = '\n'.join('  ' + ln for ln in combined.split('\n')) or '  '
    new_fm = f'name: {nm}\ndescription: |-\n{indent}\nmetadata:\n  source: xcode-27\n'
    open(skill, 'w', encoding='utf-8').write(f'---\n{new_fm}---\n' + body.lstrip('\n'))
    print(f'  {name}: description {len(combined)}/{MAX} chars')
PY

echo "Validating exported skills"
validate_dir "$STAGE" || die "validation failed — refusing to install"

SKILL_COUNT="$(find "$STAGE" -mindepth 2 -maxdepth 2 -name SKILL.md | wc -l | tr -d ' ')"

if [ "$DRY_RUN" -eq 1 ]; then
    echo "Dry run: $SKILL_COUNT skill(s) validated; not writing to $DEST"
    exit 0
fi

echo "Installing into $DEST"
mkdir -p "$DEST"
rsync -a "$STAGE/" "$DEST/"

echo "Installed $SKILL_COUNT skill(s) into $DEST"
