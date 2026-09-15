#!/usr/bin/env python3
"""Auto-fix: read the latest structured AI review on a PR, apply suggested
old->new edits for actionable severities, compile-check Go changes, commit,
push, reply with a summary, and label the PR to cap the loop at one pass.

Runs as the `fix` job of the AI Code Review workflow (pull_request trigger),
right after the `review` job posts the review. No LLM needed here — the reviewer
already supplied the exact edits.
"""
import base64
import json
import os
import re
import subprocess
import sys
import tempfile

JSON_FENCE = "ai-review-json"
LABEL = "ai-auto-fixed"
FIX_SEVERITIES = ("must_fix", "should_fix")
COMMIT_MARKER = "[ai-fix]"


def run(cmd, check=False, **kw):
    return subprocess.run(cmd, capture_output=True, text=True, check=check, **kw)


def pr_number() -> str:
    if os.environ.get("PR_NUMBER"):
        return os.environ["PR_NUMBER"]
    branch = os.environ.get("HEAD_BRANCH")
    if branch:
        r = run(
            ["gh", "pr", "list", "--head", branch, "--state", "open",
             "--json", "number", "-q", ".[0].number"],
        )
        n = r.stdout.strip()
        if n:
            return n
    sys.exit("could not determine PR number")


def head_branch() -> str:
    b = os.environ.get("HEAD_BRANCH")
    if b:
        return b
    r = run(["git", "rev-parse", "--abbrev-ref", "HEAD"])
    return r.stdout.strip() or "main"


def has_label(pr: str, repo: str) -> bool:
    r = run(["gh", "pr", "view", pr, "--json", "labels", "-q", ".labels[].name"])
    return LABEL in r.stdout.split()


def latest_review_json(pr: str, repo: str):
    r = run(["gh", "api", f"repos/{repo}/pulls/{pr}/reviews"])
    try:
        reviews = json.loads(r.stdout or "[]")
    except json.JSONDecodeError:
        return None
    for rev in reversed(reviews):
        user = (rev.get("user") or {}).get("login", "")
        # Only trust bot-authored reviews; a human can otherwise paste a fenced
        # ai-review-json block to make the fixer apply arbitrary edits.
        if not user.endswith("[bot]"):
            continue
        body = rev.get("body") or ""
        data = extract_json(body)
        if data is not None:
            return data
    return None


def extract_json(body: str):
    m = re.search(rf"```{JSON_FENCE}\n(.*?)\n```", body, re.S)
    if not m:
        return None
    try:
        return json.loads(m.group(1))
    except json.JSONDecodeError:
        return None


def apply_edit(path: str, old: str, new: str) -> bool:
    try:
        with open(path, encoding="utf-8", newline="") as f:
            src = f.read()
    except OSError:
        return False
    if not old:
        return False
    crlf = "\r\n" in src
    src_n = src.replace("\r\n", "\n")
    old_n = old.replace("\r\n", "\n")
    new_n = new.replace("\r\n", "\n")

    def write(out_n: str) -> None:
        out = out_n.replace("\n", "\r\n") if crlf else out_n
        with open(path, "w", encoding="utf-8", newline="") as f:
            f.write(out)

    # 1. Exact match (line-ending normalized).
    if old_n in src_n:
        write(src_n.replace(old_n, new_n, 1))
        return True

    # 2. Line-anchored: locate first + last non-blank line of `old` in the file
    #    (indentation + trailing whitespace tolerant) and replace the span between
    #    them. This survives indentation drift on inner lines.
    old_lines = old_n.split("\n")
    anchors = [l.strip() for l in old_lines if l.strip()]
    if anchors:
        first, last = anchors[0], anchors[-1]
        src_lines = src_n.split("\n")
        i0 = i1 = None
        matches = []
        for i, l in enumerate(src_lines):
            if l.strip() == first:
                if first == last:
                    matches.append((i, i))
                    continue
                for j in range(i + 1, len(src_lines)):
                    if src_lines[j].strip() == last:
                        matches.append((i, j))
                        break
        if len(matches) != 1:
            return False
        i0, i1 = matches[0]
        if i0 <= i1:
            if new_n.strip():
                repl = new_n.rstrip("\n").split("\n")
            else:
                repl = []  # pure deletion
            src_lines[i0 : i1 + 1] = repl
            write("\n".join(src_lines))
            return True
    return False


def post_comment(repo: str, pr: str, body: str) -> None:
    fd, tmp = tempfile.mkstemp(suffix=".json")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        json.dump({"body": body}, f)
    try:
        run(
            ["gh", "api", f"repos/{repo}/issues/{pr}/comments", "--method", "POST",
             "-H", "Content-Type: application/json", "--input", tmp],
        )
    finally:
        os.unlink(tmp)


def main() -> None:
    repo = os.environ["GITHUB_REPOSITORY"]
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    pr = pr_number()
    branch = head_branch()

    # Ensure the label exists — gh pr edit --add-label does NOT auto-create it.
    run(["gh", "label", "create", LABEL, "--color", "0366d6", "--force"])
    # Loop cap: a prior run already auto-fixed this PR.
    if has_label(pr, repo):
        print(f"Label {LABEL} present; skipping auto-fix (loop cap).")
        return
    # Claim the loop cap before doing work so concurrent runs cannot both push.
    # (Best-effort; the workflow-level concurrency group is the real mutex.)
    claim = run(["gh", "pr", "edit", pr, "--add-label", LABEL])
    if claim.returncode != 0:
        print("could not claim label; aborting to avoid duplicate pushes")
        return

    review = latest_review_json(pr, repo)
    if review is None:
        print("No structured AI review found; skipping.")
        return

    issues = [
        i for i in (review.get("issues") or [])
        if i.get("severity") in FIX_SEVERITIES and i.get("old")
    ]
    if not issues:
        print("No actionable issues; skipping.")
        return

    applied = []
    skipped = []
    changed_go = False
    for it in issues:
        path = it.get("path")
        old = it.get("old")
        new = it.get("new")
        if (
            not isinstance(path, str)
            or not isinstance(old, str)
            or not isinstance(new, str)
            or not path
            or not old
            or old == new
        ):
            skipped.append((it, "invalid or empty edit"))
            continue
        norm = os.path.normpath(path)
        real_root = os.path.realpath(os.getcwd())
        real_target = os.path.realpath(os.path.join(real_root, norm))
        if (
            os.path.isabs(path)
            or path != norm
            or norm in ("..", ".")
            or norm.startswith(".." + os.sep)
            or norm == ".git"
            or norm.startswith(".git" + os.sep)
            or not (real_target == real_root
                    or real_target.startswith(real_root + os.sep))
        ):
            skipped.append((it, "unsafe path"))
            continue
        if apply_edit(path, old, new):
            applied.append(it)
            if path.endswith(".go"):
                changed_go = True
        else:
            skipped.append((it, "old text not found in current file"))

    if not applied:
        body = (
            "## AI auto-fix: no edits applied\n\n"
            "Could not apply any suggested edits (old text not found). "
            "Leaving for human review.\n\n"
        )
        for it, why in skipped:
            body += f"- `{it.get('path','?')}` — {it.get('title','')} ({why})\n"
        post_comment(repo, pr, body)
        print("No edits applied; left for human.")
        return

    # Compile-check before committing.
    if changed_go:
        r = run(["go", "build", "-o", os.devnull, "./..."])
        if r.returncode != 0:
            body = (
                "## AI auto-fix: build failed\n\n"
                "Edits applied but `go build ./...` failed. Changes NOT committed.\n\n"
                "```\n" + (r.stderr or r.stdout)[-4000:] + "\n```\n"
            )
            post_comment(repo, pr, body)
            print("go build failed; not committing.")
            sys.exit(1)

    # Commit and push to the PR head branch.
    run(["git", "config", "user.email", "github-actions[bot]@users.noreply.github.com"])
    run(["git", "config", "user.name", "github-actions[bot]"])
    for it in applied:
        run(["git", "add", "--", it.get("path")])
    r = run(["git", "commit", "-m",
             f"fix: address AI review feedback {COMMIT_MARKER}"])
    if r.returncode != 0:
        # Nothing staged (edits were whitespace-only or already applied).
        print("Nothing to commit; skipping push.")
    else:
        # Push via credential header injected into git config for the push
        # duration only; never embed the token in argv or remote URLs.
        env = dict(os.environ)
        env["GIT_ASKPASS"] = "true"
        env["GIT_TERMINAL_PROMPT"] = "0"
        if token:
            header = base64.b64encode(f"x-access-token:{token}".encode()).decode()
            run(["git", "config", "--local", "http.https://github.com/.extraheader",
                 f"AUTHORIZATION: basic {header}"])
        try:
            r = run(["git", "push", "origin", f"HEAD:refs/heads/{branch}"], env=env)
        finally:
            run(["git", "config", "--local", "--unset", "http.https://github.com/.extraheader"])
        if r.returncode != 0:
            out = (r.stderr or r.stdout)[-4000:]
            if token:
                out = out.replace(token, "***")
            body = (
                "## AI auto-fix: push failed\n\n"
                "```\n" + out + "\n```\n"
            )
            post_comment(repo, pr, body)
            print("git push failed.")
            sys.exit(1)

    # Summary comment + loop-cap label.
    lines = ["## AI auto-fix applied", ""]
    for it in applied:
        lines.append(f"- ✅ `{it.get('path','?')}` — {it.get('title','')}")
    for it, why in skipped:
        lines.append(f"- ⏭️ `{it.get('path','?')}` — {it.get('title','')} ({why})")
    post_comment(repo, pr, "\n".join(lines))
    run(["gh", "pr", "edit", pr, "--add-label", LABEL])
    print(f"Applied {len(applied)} edits, skipped {len(skipped)}, labeled {LABEL}.")


if __name__ == "__main__":
    main()
