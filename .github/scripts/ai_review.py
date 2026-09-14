#!/usr/bin/env python3
"""AI code review: PR diff -> litellm -> pull_request_review (verdict + body).

Triggered by workflow_run (after CI passes), so it derives the PR number and
base branch from the workflow_run context instead of the pull_request context.
"""
import json
import os
import subprocess
import sys
import urllib.request


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
        "Be specific and actionable; reference file names and line numbers.\n"
        "Respond with ONLY a JSON object (no markdown fences, no prose):\n"
        '{"verdict": "APPROVE" or "REQUEST_CHANGES", "body": "concise markdown review"}\n\n'
        f"DIFF:\n{diff}"
    )

    payload = {
        "model": "deepseek-v4-pro",
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": 8000,
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

    content = resp["choices"][0]["message"].get("content") or ""
    review = parse_review(content)
    verdict = review.get("verdict", "REQUEST_CHANGES")
    body = review.get("body") or "(no review body)"

    # GITHUB_TOKEN cannot submit an APPROVE review (GitHub blocks bot approvals).
    # Request changes when issues are found; otherwise post a non-blocking
    # comment. The required human review remains the final merge gate.
    if verdict == "REQUEST_CHANGES":
        event = "REQUEST_CHANGES"
    else:
        event = "COMMENT"

    subprocess.run(
        [
            "gh", "api", f"repos/{repo}/pulls/{pr_number}/reviews",
            "--method", "POST",
            "--field", f"event={event}",
            "--field", f"body={body}",
        ],
        check=True,
    )
    print(f"Review posted: {event}")


if __name__ == "__main__":
    main()
