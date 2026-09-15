#!/usr/bin/env python3
"""Auto-fix: read the latest structured AI review on a PR, apply suggested
old->new edits for actionable severities, compile-check Go changes, commit,
push, reply with a summary, and label the PR to cap the loop at one pass.

Runs as the `fix` job of the AI Code Review workflow (pull_request trigger),
right after the `review` job posts the review. No LLM needed here — the reviewer
already supplied the exact edits.
"""
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
    if r.returncode != 0:
        # Fail closed: unknown label state is treated as present to preserve the
        # loop cap rather than risk a duplicate fix pass.
        return True
    return LABEL in (r.stdout or "").splitlines()


def latest_review_json(pr: str, repo: str):
    r = run(["gh", "api", f"repos/{repo}/pulls/{pr}/reviews"])
    try:
        reviews = json.loads(r.stdout or "[]")
    except json.JSONDecodeError:
        return None
    for rev in reversed(reviews):
        # Skip reviews from a prior push (stale diff) — only trust the review
        # for the current head commit.
        head_sha = os.environ.get("HEAD_SHA", "")
        if head_sha and rev.get("commit_id") and rev["commit_id"] != head_sha:
            continue
        expected = os.environ.get("REVIEW_BOT_LOGIN", "github-actions[bot]")
        user = (rev.get("user") or {}).get("login", "")
        # Only trust the specific bot that our workflow uses; any other GitHub
        # App installed on the repo can also end with [bot] and could inject
        # edits.
        if user != expected:
            continue
        body = rev.get("body") or ""
        data = extract_json(body)
        if data is not None:
            return data
    return None


def extract_json(body: str):
    body = body.replace("\r\n", "\n")
    m = re.search(rf"```{JSON_FENCE}\s*\n(.*?)\n```", body, re.S)
    if not m:
        return None
    try:
        # Reverse ai_review.build_body's backtick sanitization (``` -> ``\u200b``)
        # so code snippets containing backticks survive the round-trip.
        return json.loads(m.group(1).replace("``\u200b``", "```"))
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
        dirn = os.path.dirname(os.path.abspath(path)) or "."
        fd, tmp = tempfile.mkstemp(dir=dirn, prefix=".ai_fix.")
        try:
            with os.fdopen(fd, "w", encoding="utf-8", newline="") as f:
                f.write(out)
            os.replace(tmp, path)
        except Exception:
            try:
                os.unlink(tmp)
            except OSError:
                pass
            raise

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
        # Verify inner lines: every non-blank line of `old` must appear in order
        # within the matched span; otherwise the first/last anchors are
        # coincidental and we'd silently rewrite the wrong block.
        inner = [l.strip() for l in old_lines if l.strip()]
        span = [l.strip() for l in src_lines[i0 : i1 + 1]]
        j = 0
        for l in span:
            if j < len(inner) and l == inner[j]:
                j += 1
        if j != len(inner):
            return False
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
    # The label is the loop cap (one fix pass), not a mutex. Real mutual
    # exclusion comes from the workflow-level concurrency group (one run/PR).

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
            os.path.isabs(norm)
            or norm in ("..", ".")
            or norm.split(os.sep, 1)[0] == ".."
            or norm == ".git"
            or norm.startswith(".git" + os.sep)
            or norm == ".github"
            or norm.startswith(".github" + os.sep)  # refuse to self-edit workflow/scripts
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
        # `go build -o <file> ./...` fails on multi-package builds; build with
        # no -o (binaries land in package dirs, not staged since we `git add`
        # only the edited paths).
        r = run(["go", "build", "./..."])
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
        p = it.get("path")
        if isinstance(p, str) and p:
            run(["git", "add", "--", p])
    r = run(["git", "commit", "-m",
             f"fix: address AI review feedback {COMMIT_MARKER}"])
    if r.returncode != 0:
        # Nothing staged (edits were whitespace-only or already applied).
        print("Nothing to commit; skipping push.")
    else:
        # Push via an askpass helper so the token is never exposed in argv
        # (git config --extraheader would put it in /proc/<pid>/cmdline).
        env = dict(os.environ)
        env["GIT_TERMINAL_PROMPT"] = "0"
        askpass = None
        if token:
            fd, askpass = tempfile.mkstemp(prefix="gh-askpass-")
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                f.write("#!/bin/sh\necho \"$GH_ASKPASS_TOKEN\"\n")
            os.chmod(askpass, 0o700)
            env["GIT_ASKPASS"] = askpass
            env["GH_ASKPASS_TOKEN"] = token
        try:
            r = run(["git", "push", f"https://x-access-token@github.com/{repo}.git",
                     f"HEAD:refs/heads/{branch}"], env=env)
        finally:
            if askpass:
                try:
                    os.unlink(askpass)
                except OSError:
                    pass
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
