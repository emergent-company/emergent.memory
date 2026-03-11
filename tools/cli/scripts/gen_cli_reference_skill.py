#!/usr/bin/env python3
"""Prepend SKILL.md frontmatter to the generated CLI reference body."""
import sys
import os

skill_dir = os.path.join("tools", "cli", "internal", "skillsfs", "skills", "memory-cli-reference")
body_file = os.path.join(skill_dir, "SKILL.md.body")
skill_file = os.path.join(skill_dir, "SKILL.md")

header = """---
name: memory-cli-reference
description: Full Memory CLI command reference with all subcommands and flags. Use when you need exact command syntax, flag names, or usage examples for any `memory` CLI command.
metadata:
  author: emergent
  version: "1.0"
---

This skill contains the complete `memory` CLI command reference, auto-generated from the binary.

Use this when you need to look up:
- Exact subcommand names (e.g. `memory agents get-run`, `memory provider configure-project`)
- Available flags and their types for any command
- Usage examples embedded in the help text
- Which subcommands exist under a parent command

"""

with open(body_file) as f:
    body = f.read()

with open(skill_file, "w") as f:
    f.write(header + body)

os.remove(body_file)
print(f"Regenerated {skill_file}")
