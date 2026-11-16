#!/usr/bin/env python3
"""Self-test for `port_mkdocs.py --check` (#954).

The check is the only thing that stops the committed Astro port drifting from
`docs/site`, so its failure modes are pinned here instead of being trusted:
a stale page, a page the port writes but the tree lost, an orphan the converter
no longer generates, and a hand edit to a generated page must all fail; editing
a hand-written page must not; and `--check` must never write.

The fixture is a throwaway `docs/site` with one stub page per entry the
converter expects, so the test does not depend on the real docs. Stdlib only,
no npm — the converter reads nothing else at build time.

Run: python3 docs-astro/scripts/port_mkdocs_test.py
"""
import contextlib
import io
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "port_mkdocs.py"
sys.path.insert(0, str(HERE))

import port_mkdocs as port  # noqa: E402  (path set above)


def write_stub_site(root: Path):
    """A throwaway source tree with one minimal page per expected conversion."""
    root.mkdir(parents=True, exist_ok=True)
    (root / "go-sdk" / "reference").mkdir(parents=True, exist_ok=True)
    pages = port.build_pages(root)
    for src_rel, _dst_rel, title in pages:
        path = root / src_rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(f"# {title}\n\nStub body for the port self-test.\n")
    return pages


class PortCheckTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="port-mkdocs-test-"))
        self.addCleanup(shutil.rmtree, self.tmp, ignore_errors=True)
        self.src = self.tmp / "site"
        self.dst = self.tmp / "docs"
        write_stub_site(self.src)
        self.generated, _stats = port.generate(self.src)
        # `write` and `generate` narrate on stdout; the assertions do not need it.
        with contextlib.redirect_stdout(io.StringIO()):
            port.write(self.generated, self.dst)

    def page(self, rel: str) -> Path:
        return self.dst / rel

    # ---- what must pass ---------------------------------------------------- #

    def test_clean_tree_passes(self):
        self.assertEqual(port.drift(self.generated, self.dst), [])

    def test_hand_written_pages_are_excluded(self):
        for rel in sorted(port.HANDWRITTEN):
            path = self.page(rel)
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("# Hand written\n\nNobody generates this page.\n")
        self.assertEqual(port.drift(self.generated, self.dst), [])

    # ---- what must fail ---------------------------------------------------- #

    def test_hand_edit_to_generated_page_is_stale(self):
        # docs/site is authoritative: editing the MDX is drift, not a fix.
        path = self.page("cli.mdx")
        path.write_text(path.read_text() + "\nhand edit\n")
        self.assertEqual(port.drift(self.generated, self.dst), [("stale", "cli.mdx")])

    def test_source_change_without_regenerating_is_stale(self):
        # Refresh the source, then convert it the way CI does: the committed
        # page no longer matches what the current docs/site produces.
        (self.src / "cli.md").write_text(
            "# CLI\n\nChanged upstream, port not re-run.\n"
        )
        regenerated, _stats = port.generate(self.src)
        self.assertEqual(port.drift(regenerated, self.dst), [("stale", "cli.mdx")])

    def test_generated_page_missing_from_tree(self):
        self.page("cli.mdx").unlink()
        self.assertEqual(port.drift(self.generated, self.dst), [("missing", "cli.mdx")])

    def test_unexpected_page_in_tree_is_orphaned(self):
        self.page("old-page.mdx").write_text("stale leftover\n")
        self.assertEqual(port.drift(self.generated, self.dst), [("orphaned", "old-page.mdx")])

    def test_page_dropped_from_converter_still_fails(self):
        # Dropping a page from the conversion list must not be a way to satisfy
        # the gate: the committed page it stops generating is an orphan.
        remaining = [g for g in self.generated if g[1] != "cli.mdx"]
        self.assertEqual(port.drift(remaining, self.dst), [("orphaned", "cli.mdx")])

    # ---- exit codes, through the CLI the gate runs ------------------------- #

    def run_check(self, src: Path = None):
        return subprocess.run(
            [
                sys.executable,
                str(SCRIPT),
                "--check",
                "--src",
                str(src if src is not None else self.src),
                "--dst",
                str(self.dst),
            ],
            capture_output=True,
            text=True,
        )

    def test_exit_zero_when_in_sync(self):
        result = self.run_check()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("match", result.stdout)

    def test_exit_one_and_no_writes_on_drift(self):
        path = self.page("cli.mdx")
        path.write_text("hand edit\n")
        result = self.run_check()
        self.assertEqual(result.returncode, 1)
        self.assertIn("DRIFT", result.stderr)
        self.assertIn("cli.mdx", result.stderr)
        self.assertIn("Regenerate the port", result.stderr)
        # The check reports drift; it must never be the thing that fixes it.
        self.assertEqual(path.read_text(), "hand edit\n")

    def test_exit_two_when_the_source_tree_is_absent(self):
        result = self.run_check(src=self.tmp / "no-such-tree")
        self.assertEqual(result.returncode, 2)
        self.assertIn("cannot proceed", result.stderr)


if __name__ == "__main__":
    unittest.main()
