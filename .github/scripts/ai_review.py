#!/usr/bin/env python3
"""AI code review: PR diff -> litellm -> structured review (verdict + issues,
each with an exact old->new suggested edit), posted as a single review whose
body carries a machine-readable JSON block that the auto-fixer consumes.

Triggered on pull_request events; the PR number and head branch come from the
workflow's env (PR_NUMBER, HEAD_BRANCH), and the base is resolved via `gh pr view`.
"""
import json
import os
import subprocess
import sys
import tempfile
import urllib.request

# Fence tag around the machine-readable JSON block in the review body.
JSON_FENCE = "ai-review-json"
# Severities the auto-fixer acts on. "nit" is left for humans.
FIX_SEVERITIES = ("must_fix", "should_fix")


def get_pr_number() -> str:
    if os.environ.get("PR_NUMBER"):
        return os.environ["PR_NUMBER"]
    branch = os.environ.get("HEAD_BRANCH")
    if branch:
        r = subprocess.run(
            ["gh", "pr", "list", "--head", branch, "--state", "open",
             "--json", "number", "-q", ".[0].number"],
            capture_output=True, text=True,
        )
        n = r.stdout.strip()
        if n:
            return n
    sys.exit("could not determine PR number")


def get_base(pr: str) -> str:
    r = subprocess.run(
        ["gh", "pr", "view", pr, "--json", "baseRefName", "-q", ".baseRefName"],
        capture_output=True, text=True,
    )
    base = r.stdout.strip()
    return base or "main"


def get_diff(base: str) -> str:
    # Ensure the base branch ref is present, then diff base...HEAD.
    subprocess.run(
        ["git", "fetch", "--no-tags", "origin", base],
        capture_output=True,
    )
    r = subprocess.run(
        ["git", "diff", f"origin/{base}...HEAD"],
        capture_output=True, text=True,
    )
    if r.returncode != 0:
        sys.exit(f"git diff failed: {r.stderr}")
    return r.stdout


def parse_review(content: str) -> dict:
    s = (content or "").strip()
    if s.startswith("```"):
        s = s.split("\n", 1)[-1]
        if s.rstrip().endswith("```"):
            s = s.rstrip()[:-3]
    i, j = s.find("{"), s.rfind("}")
    if i != -1 and j != -1 and j > i:
        s = s[i : j + 1]
    try:
        return json.loads(s)
    except json.JSONDecodeError:
        return {"verdict": "COMMENT", "body": s}


def build_body(review: dict) -> str:
    issues = review.get("issues") or []
    if not issues:
        md = "No actionable issues found."
    else:
        lines = []
        for it in issues:
            sev = it.get("severity", "should_fix")
            title = it.get("title", "issue")
            path = it.get("path", "?")
            note = it.get("note", "")
            safe_title = str(title).replace("`", "\\`").replace("\n", " ")
            safe_note = str(note).replace("\n", " ")
            safe_path = str(path).replace("`", "\\`")
            lines.append(f"- **[{sev}]** `{safe_path}` — {safe_title}")
            if safe_note:
                lines.append(f"  - {safe_note}")
        md = "\n".join(lines)
    payload = json.dumps(review, indent=2)
    # Guard against the payload containing a closing fence sequence.
    payload = payload.replace("```", "``\u200b``")
    return f"{md}\n\n```{JSON_FENCE}\n{payload}\n```\n"


def get_head_sha() -> str:
    r = subprocess.run(["git", "rev-parse", "HEAD"], capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit("could not determine head SHA")
    return r.stdout.strip()


def locate_span(path: str, old: str):
    """Return (start, end) 1-based line numbers of `old` in the file, or None."""
    try:
        with open(path, encoding="utf-8", newline="") as f:
            src = f.read()
    except OSError:
        return None
    old_n = old.replace("\r\n", "\n").rstrip("\n")
    src_n = src.replace("\r\n", "\n")
    idx = src_n.find(old_n)
    if idx == -1:
        return None
    start = src_n[:idx].count("\n") + 1
    end = start + old_n.count("\n")
    return (start, end)


def post_inline_suggestions(repo: str, pr: str, commit_id: str, issues) -> None:
    """Post each issue as a line-anchored review comment with a ```suggestion
    block so GitHub renders a one-click 'Commit suggestion', like Copilot."""
    for it in issues:
        path = it.get("path")
        old = it.get("old")
        new = it.get("new")
        if (
            not path
            or not isinstance(old, str) or not old
            or not isinstance(new, str) or not new.strip()
        ):
            continue
        span = locate_span(path, old)
        if not span:
            print(f"skip inline suggestion for {path}: anchor not found", file=sys.stderr)
            continue
        start, end = span
        sev = it.get("severity", "should_fix")
        title = it.get("title", "")
        note = it.get("note", "")
        body = f"**[{sev}]** {title}"
        if note:
            body += f"\n\n{note}"
        body += f"\n\n```suggestion\n{new.rstrip(chr(10))}\n```"
        payload = {
            "body": body,
            "path": path,
            "line": end,
            "side": "RIGHT",
            "commit_id": commit_id,
        }
        if start != end:
            payload["start_line"] = start
            payload["start_side"] = "RIGHT"
        fd, tmp = tempfile.mkstemp(suffix=".json")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(payload, f)
        try:
            subprocess.run(
                ["gh", "api", f"repos/{repo}/pulls/{pr}/comments",
                 "--method", "POST",
                 "-H", "Content-Type: application/json",
                 "--input", tmp],
                check=False,  # 422 if line isn't in the diff — flat review still carries it
            )
        finally:
            os.unlink(tmp)


def main() -> None:
    pr_number = get_pr_number()
    base = get_base(pr_number)
    repo = os.environ["GITHUB_REPOSITORY"]
    base_url = os.environ["LITELLM_BASE_URL"].rstrip("/")
    key = os.environ["LITELLM_API_KEY"]

    diff = get_diff(base)
    if not diff.strip():
        print("No diff to review; skipping.")
        return

    max_diff = 15000
    if len(diff) > max_diff:
        diff = diff[:max_diff] + "\n...(truncated)..."

    prompt = (
        "You are a senior software engineer performing a rigorous code review.\n"
        "Review the PR diff for: correctness bugs, security issues, performance "
        "problems, error-handling gaps, race conditions, and missing tests.\n"
        "Respond with ONLY a JSON object (no markdown fences, no prose).\n"
        "JSON schema:\n"
        '{\n'
        '  "verdict": "APPROVE" or "REQUEST_CHANGES",\n'
        '  "issues": [\n'
        '    {\n'
        '      "path": "<relative file path, e.g. apps/server/domain/.../foo.go>",\n'
        '      "severity": "must_fix" | "should_fix" | "nit",\n'
        '      "title": "<one-line summary>",\n'
        '      "old": "<EXACT verbatim contiguous lines from the CURRENT file, '
        "copied character-for-character, preserving indentation>\",\n"
        '      "new": "<corrected replacement lines>",\n'
        '      "note": "<why this matters>"\n'
        '    }\n'
        '  ]\n'
        '}\n'
        "Rules:\n"
        '- "old" is the current code to replace. Keep it minimal: the shortest '
        'unambiguous snippet (1-3 lines) copied verbatim from the diff, preserving '
        'indentation exactly. The fixer locates it by first+last line, so leading '
        'whitespace must match. If you cannot reproduce it exactly, set "old" to "" '
        '(the fixer will skip it).\n'
        '- "new" is the exact replacement text. If "new" equals "old", omit the issue.\n'
        '- "severity": "must_fix" for correctness/security/breakage, "should_fix" for '
        'logic/error-handling/perf, "nit" for style.\n'
        "Only report real issues; if none, return an empty issues array and verdict "
        '"APPROVE".\n\n'
        f"DIFF:\n{diff}"
    )

    payload = {
        # deepseek-flash is a thinking model. Disable thinking via extra_body —
        # LiteLLM filters top-level `thinking` when the deployment isn't a native
        # deepseek provider (drop_params), but extra_body is merged verbatim.
        "model": "deepseek-v4-flash",
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 8000,
        "extra_body": {"thinking": {"type": "disabled"}},
    }
    req = urllib.request.Request(
        f"{base_url}/chat/completions",
        data=json.dumps(payload).encode(),
        headers={
            "Authorization": f"Bearer {key}",
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(req, timeout=300) as r:
        resp = json.loads(r.read())

    msg = resp["choices"][0]["message"]
    content = msg.get("content") or ""
    if not content.strip():
        sys.exit("review model returned empty content")
    review = parse_review(content)
    # Only keep issues whose path appears in the diff; the fixer will otherwise
    # happily edit unrelated files based on a hallucinated path.
    changed = set()
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            changed.add(line[6:].strip())
    issues = [
        i for i in (review.get("issues") or [])
        if i.get("path") in changed
    ]
    review["issues"] = issues
    verdict = review.get("verdict", "REQUEST_CHANGES")

    # GITHUB_TOKEN cannot submit an APPROVE review (GitHub blocks bot approvals).
    # Request changes when actionable issues exist; otherwise post a non-blocking
    # comment. The required human review remains the final merge gate.
    actionable = any(
        i.get("severity") in FIX_SEVERITIES and i.get("old") for i in issues
    )
    if actionable or verdict == "REQUEST_CHANGES":
        event = "REQUEST_CHANGES"
    else:
        event = "COMMENT"

    body = build_body(review)

    # Post via a JSON file (--input) instead of --field: large bodies with
    # special characters break gh's --field type coercion.
    fd, tmp = tempfile.mkstemp(suffix=".json")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump({"event": event, "body": body}, f)
    try:
        subprocess.run(
            [
                "gh", "api", f"repos/{repo}/pulls/{pr_number}/reviews",
                "--method", "POST",
                "-H", "Content-Type: application/json",
                "--input", tmp,
            ],
            check=True,
        )
    finally:
        os.unlink(tmp)

    # Post line-anchored inline suggestions (one-click 'Commit suggestion'),
    # like Copilot. Best-effort; a 422 line mismatch just skips that comment.
    post_inline_suggestions(repo, pr_number, get_head_sha(), issues)
    print(f"Review posted: {event} ({len(issues)} issues)")


if __name__ == "__main__":
    main()
