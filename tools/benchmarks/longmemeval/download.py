#!/usr/bin/env python3
"""
LongMemEval download — fetches the dataset from HuggingFace (no auth required).

Usage:
    python download.py [--split oracle|s|m] [--output data/]
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


SPLIT_MAP = {
    "oracle": "longmemeval_oracle",
    "s":      "longmemeval_s_cleaned",
    "m":      "longmemeval_m_cleaned",
}

HF_REPO = "xiaowu0162/longmemeval-cleaned"


def main() -> None:
    ap = argparse.ArgumentParser(description="Download LongMemEval from HuggingFace")
    ap.add_argument("--split", choices=["oracle", "s", "m"], default="oracle",
                    help="Which split to download (oracle=min haystack, s=~40 sessions, m=~500)")
    ap.add_argument("--output", default="data", help="Output directory")
    args = ap.parse_args()

    try:
        from datasets import load_dataset  # type: ignore
    except ImportError:
        print("ERROR: pip install datasets", file=sys.stderr)
        sys.exit(1)

    out_dir = Path(args.output)
    out_dir.mkdir(parents=True, exist_ok=True)

    split_name = SPLIT_MAP[args.split]
    out_path = out_dir / f"{split_name}.json"

    if out_path.exists():
        print(f"Already downloaded: {out_path}")
        return

    print(f"Downloading {HF_REPO} split={split_name} ...")
    try:
        # Use streaming=True to avoid loading broken splits (m split pyarrow overflow)
        ds = load_dataset(HF_REPO, split=split_name, streaming=True)
        records = [dict(row) for row in ds]
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)

    records = [dict(row) for row in ds]
    with open(out_path, "w") as f:
        json.dump(records, f, indent=2)

    print(f"Saved {len(records)} records → {out_path}")


if __name__ == "__main__":
    main()
