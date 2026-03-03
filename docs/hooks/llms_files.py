"""
MkDocs hook: copy llms reference files into the built site root
so they are served as raw Markdown at:
  /llms.md
  /llms-go-sdk.md
  /llms-swift-sdk.md
"""
import shutil
from pathlib import Path


def on_post_build(config):
    site_dir = Path(config["site_dir"])
    repo_root = Path(config["docs_dir"]).parent  # docs/site -> docs -> repo root
    # docs_dir is docs/site, so parent is docs/, parent.parent is repo root
    docs_root = repo_root.parent

    llms_files = [
        "docs/llms.md",
        "docs/llms-go-sdk.md",
        "docs/llms-swift-sdk.md",
    ]

    for rel_path in llms_files:
        src = docs_root / rel_path
        dst = site_dir / Path(rel_path).name  # copy to site root, keep filename
        if src.exists():
            shutil.copy2(src, dst)
            print(f"mkdocs-llms-hook: copied {src.name} → {dst}")
        else:
            print(f"mkdocs-llms-hook: WARNING {src} not found, skipping")
