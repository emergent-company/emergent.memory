#!/usr/bin/env python3
"""AI code review: per-file diff chunks -> litellm -> one merged review.

Triggered by pull_request_target; loads from the base branch (trusted) and reads
only the PR diff. Reviews every changed file by chunking the diff into
model-sized pieces instead of truncating the whole diff (so no file is skipped).
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

# Per-chunk diff budget (chars) and the merged-issues cap across all chunks.
CHUNK_CHARS = 40000
MAX_TOTAL_ISSUES = 8

# Files we skip: generated code and dependency lockfiles (bloat, not hand-written).
GENERATED_SUFFIXES = ("_templ.go", ".gen.go", ".pb.go", ".pb.gw.go", ".graphql.go")
SKIP_BASENAMES = (
    "go.sum", "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb",
)


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


def get_changed_files(base: str):
    r = subprocess.run(
        ["git", "diff", "--name-only", f"origin/{base}...pr-head"],
        capture_output=True, text=True,
    )
    if r.returncode != 0:
        sys.exit(f"git diff --name-only failed: {r.stderr}")
    return [f for f in r.stdout.splitlines() if f.strip()]


def is_skip(path: str) -> bool:
    name = path.rsplit("/", 1)[-1]
    return name in SKIP_BASENAMES or any(path.endswith(s) for s in GENERATED_SUFFIXES)


def file_diff(base: str, path: str) -> str:
    r = subprocess.run(
        ["git", "diff", f"origin/{base}...pr-head", "--", path],
        capture_output=True, text=True,
    )
    return r.stdout if r.returncode == 0 else ""


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


def build_prompt(diff_text: str) -> str:
    return (
        "You are a senior software engineer performing a rigorous code review.\n"
        "Review the PR diff for: correctness bugs, security issues, performance "
        "problems, error-handling gaps, race conditions, and missing tests.\n"
        "Respond with ONLY a JSON object (no markdown fences, no prose):\n"
        '{"verdict": "APPROVE" or "REQUEST_CHANGES", "issues": ['
        '{"path": "<relative file path>", "severity": "must_fix"|"should_fix"|"nit", '
        '"title": "<one-line summary>", "note": "<what is wrong and where>"}]}\n'
        "Rules:\n"
        '- Only report a finding when the code is actually wrong or clearly risky. '
        'If a pattern is acceptable or "correct but could be nicer", do NOT report '
        'it — noise erodes trust.\n'
        '- "note" must cite the specific line(s) or behavior, state what is wrong, '
        'and give a concrete correct fix. Avoid hedge words ("may", "consider", '
        '"verify") unless the finding is genuinely uncertain.\n'
        '- "severity": "must_fix" = definite bug/security/breakage that ships broken; '
        '"should_fix" = a clear, worthwhile improvement; "nit" = minor style. When in '
        'doubt, downgrade or drop the finding.\n'
        '- Report at most 4 issues, ordered by severity then impact. Quality over '
        'quantity. If nothing is actually wrong, return an empty issues array and '
        'verdict "APPROVE".\n\n'
        f"DIFF:\n{diff_text}"
    )


def call_model(prompt: str, base_url: str, key: str) -> dict:
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
    return parse_review(content)


def build_body(issues) -> str:
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

    subprocess.run(["git", "fetch", "--no-tags", "origin", base], capture_output=True)

    files = [f for f in get_changed_files(base) if not is_skip(f)]
    if not files:
        print("No reviewable files; skipping.")
        return

    # Group files into chunks whose total diff fits within CHUNK_CHARS.
    chunks = []
    current = []
    current_len = 0
    for path in files:
        d = file_diff(base, path)
        if not d.strip():
            continue
        if current and current_len + len(d) > CHUNK_CHARS:
            chunks.append(current)
            current = []
            current_len = 0
        current.append((path, d))
        current_len += len(d)
    if current:
        chunks.append(current)

    # Review each chunk, collecting issues across the whole PR.
    all_issues = []
    for chunk in chunks:
        diff_text = "\n".join(d for _, d in chunk)
        review = call_model(build_prompt(diff_text), base_url, key)
        all_issues.extend(review.get("issues") or [])

    # Keep only issues whose path was actually reviewed; dedupe; cap; order.
    reviewable = {p for chunk in chunks for p, _ in chunk}
    seen = set()
    deduped = []
    for it in all_issues:
        p = it.get("path")
        if p not in reviewable:
            continue
        k = (p, it.get("title", ""))
        if k in seen:
            continue
        seen.add(k)
        deduped.append(it)
    order = {"must_fix": 0, "should_fix": 1, "nit": 2}
    deduped.sort(key=lambda it: order.get(it.get("severity"), 1))
    issues = deduped[:MAX_TOTAL_ISSUES]

    blocking = any(i.get("severity") in BLOCKING_SEVERITIES for i in issues)
    event = "REQUEST_CHANGES" if blocking else "COMMENT"

    body = build_body(issues)

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
    print(f"Review posted: {event} ({len(issues)} issues from {len(chunks)} chunk(s))")


if __name__ == "__main__":
    main()
