#!/usr/bin/env python3
"""AI code review: PR diff -> litellm -> a single review (verdict + findings).

Triggered by pull_request_target; loads from the base branch (trusted) and reads
only the PR diff. This is intentionally simple: a readable review for humans.
No auto-fix, no machine-readable payload, no inline-suggestion machinery.
"""
import json
import os
import subprocess
import sys
import tempfile
import urllib.request

# Severities that flip the review to REQUEST_CHANGES (nits stay non-blocking).
BLOCKING_SEVERITIES = ("must_fix", "should_fix")


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
    # pr-head is fetched by the workflow from pull/{n}/head.
    subprocess.run(["git", "fetch", "--no-tags", "origin", base], capture_output=True)
    r = subprocess.run(
        ["git", "diff", f"origin/{base}...pr-head"], capture_output=True, text=True,
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
        return {"verdict": "COMMENT", "issues": [], "body": s}


def build_body(review: dict) -> str:
    issues = review.get("issues") or []
    if not issues:
        return "No issues found."
    lines = []
    for it in issues:
        sev = it.get("severity", "should_fix")
        title = it.get("title", "issue")
        path = it.get("path", "?")
        note = it.get("note", "")
        lines.append(f"- **[{sev}]** `{path}` — {title}")
        if note:
            lines.append(f"  - {note}")
    return "\n".join(lines)


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
    truncated = len(diff) > max_diff
    if truncated:
        diff = diff[:max_diff] + "\n...(truncated)..."

    prompt = (
        "You are a senior software engineer performing a rigorous code review.\n"
        "Review the PR diff for: correctness bugs, security issues, performance "
        "problems, error-handling gaps, race conditions, and missing tests.\n"
        "Respond with ONLY a JSON object (no markdown fences, no prose):\n"
        '{"verdict": "APPROVE" or "REQUEST_CHANGES", "issues": ['
        '{"path": "<relative file path>", "severity": "must_fix"|"should_fix"|"nit", '
        '"title": "<one-line summary>", "note": "<what is wrong and where>"}]}\n'
        "Rules:\n"
        '- "note" must be specific and actionable: cite the file and the affected '
        "line(s)/behavior, and say what a correct fix would look like.\n"
        '- "severity": "must_fix" for correctness/security/breakage, "should_fix" '
        'for logic/error-handling/perf, "nit" for style.\n'
        "Only report real issues; if none, return an empty issues array and verdict "
        '"APPROVE".\n\n'
        f"DIFF:\n{diff}"
    )

    payload = {
        # deepseek-v4-flash is a thinking model; disable thinking via extra_body
        # (LiteLLM drops a top-level `thinking` field for non-deepseek providers).
        "model": "deepseek-v4-flash",
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 8000,
        "extra_body": {"thinking": {"type": "disabled"}},
    }
    req = urllib.request.Request(
        f"{base_url}/chat/completions",
        data=json.dumps(payload).encode(),
        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=300) as r:
        resp = json.loads(r.read())

    content = resp["choices"][0]["message"].get("content") or ""
    if not content.strip():
        sys.exit("review model returned empty content")
    review = parse_review(content)

    # Keep only issues whose path appears in the diff (avoid hallucinated paths).
    changed = set()
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            changed.add(line[6:].strip())
    issues = [i for i in (review.get("issues") or []) if i.get("path") in changed]
    review["issues"] = issues

    blocking = any(i.get("severity") in BLOCKING_SEVERITIES for i in issues)
    event = "REQUEST_CHANGES" if blocking else "COMMENT"

    body = build_body(review)
    if truncated:
        body += f"\n\n> ⚠️ Diff truncated at {max_diff} chars — files beyond this limit were not reviewed.\n"

    fd, tmp = tempfile.mkstemp(suffix=".json")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump({"event": event, "body": body}, f)
    try:
        subprocess.run(
            ["gh", "api", f"repos/{repo}/pulls/{pr_number}/reviews",
             "--method", "POST", "-H", "Content-Type: application/json", "--input", tmp],
            check=True,
        )
    finally:
        os.unlink(tmp)
    print(f"Review posted: {event} ({len(issues)} issues)")


if __name__ == "__main__":
    main()
