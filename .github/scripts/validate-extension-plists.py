#!/usr/bin/env python3
"""Fail when an app extension's Info.plist declares no activation rule.

App Store Connect rejects a build whose app extension has no
``NSExtensionAttributes > NSExtensionActivationRule`` with error 90360 — and it
only says so at the upload, after the archive and the export have both
succeeded and every other check was green. The share extension shipped that way
on v0.93.0 (#480), so the declaration is checked here, before the release.

Nothing else can see this defect: ``ios-test`` builds and runs on a simulator,
which does not validate extension activation, and ``ios-release`` archives and
exports successfully with a malformed rule. There is no Swift toolchain and no
macOS runner involved — ``plistlib`` reads both XML and binary plists.

This script is the single implementation of the guard. It is called from the
required ``ci`` job (``.github/workflows/ci.yml``), because a check that lives
only in an advisory workflow can be skipped by a merge: #481 merged with its own
guard still QUEUED (#482). Move the invocation, never the logic.

Usage:
    validate-extension-plists.py [ROOT ...]      # default root: apps/ios

Exit codes: 0 the invariant holds, 1 a violation (or a vacuous scan), 2 bad root.
"""

import glob
import os
import plistlib
import sys

DEFAULT_ROOTS = ["apps/ios"]


def check_root(root, bad, counts):
    """Scan ROOT for Info.plists, appending violations to BAD."""
    if not os.path.isdir(root):
        print(f"error: no such directory: {root}", file=sys.stderr)
        return 2
    pattern = os.path.join(root, "**", "Info.plist")
    for path in sorted(glob.glob(pattern, recursive=True)):
        counts["plists"] += 1
        with open(path, "rb") as handle:
            info = plistlib.load(handle)
        extension = info.get("NSExtension")
        if not extension:
            continue  # the app's own Info.plist, not an extension
        counts["extensions"] += 1
        attributes = extension.get("NSExtensionAttributes") or {}
        if not attributes.get("NSExtensionActivationRule"):
            bad.append(path)
    return 0


def main(argv):
    roots = argv or DEFAULT_ROOTS
    bad = []
    counts = {"plists": 0, "extensions": 0}

    for root in roots:
        rc = check_root(root, bad, counts)
        if rc:
            return rc

    if bad:
        print(
            "NSExtensionActivationRule missing or empty — "
            "App Store Connect rejects these builds with 90360:"
        )
        for path in bad:
            print(f"  {path}")
        print(
            "The declaration keys go inside "
            "NSExtensionAttributes > NSExtensionActivationRule, "
            "not directly under NSExtensionAttributes."
        )
        return 1

    # A scan that matches no extension plist passes vacuously: a renamed or
    # moved extension would silently stop being covered, which is the same
    # class of bug as #482 (a guard that does not gate). Fail and make someone
    # decide, rather than report a green check that checks nothing.
    if counts["extensions"] == 0:
        print(
            "error: no extension Info.plist found under "
            f"{' '.join(roots)} (scanned {counts['plists']} Info.plist file(s)); "
            "the activation-rule guard would assert nothing. "
            "Update the roots passed to this script if the extensions moved.",
            file=sys.stderr,
        )
        return 1

    print(
        "extension Info.plists ok "
        f"({counts['extensions']} extension plist(s), "
        f"{counts['plists']} Info.plist(s) scanned)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
