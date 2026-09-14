#!/usr/bin/env python3
"""AI code review: PR diff -> litellm -> pull_request_review (verdict + body)."""
import json
import os
import subprocess
import sys
import urllib.request


def get_diff(base: str) -> str:
    r = subprocess.run(
        ["git", "diff", f"origin/{base}...HEAD"],
        capture_output=True, text=True,
    )
    if r.returncode != 0:
        sys.exit(f"git diff failed: {r.stderr}")
    return r.stdout


def parse_review(content: str) -> dict:
    s = (content or "").strip()
    # strip markdown fences if present
    if s.startswith("```"):
        s = s.split("\n", 1)[-1]
        if s.rstrip().endswith("```"):
            s = s.rstrip()[:-3]
    # extract first { ... last }
    i, j = s.find("{"), s.rfind("}")
    if i != -1 and j != -1 and j > i:
        s = s[i : j + 1]
    try:
        return json.loads(s)
    except json.JSONDecodeError:
        return {"verdict": "COMMENT", "body": s}


def main() -> None:
    base = os.environ.get("GITHUB_BASE_REF", "main")
    pr_number = os.environ["PR_NUMBER"]
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
    if verdict not in ("APPROVE", "REQUEST_CHANGES", "COMMENT"):
        verdict = "REQUEST_CHANGES"
    body = review.get("body") or "(no review body)"

    subprocess.run(
        [
            "gh", "api", f"repos/{repo}/pulls/{pr_number}/reviews",
            "--method", "POST",
            "--field", f"event={verdict}",
            "--field", f"body={body}",
        ],
        check=True,
    )
    print(f"Review posted: {verdict}")


if __name__ == "__main__":
    main()
