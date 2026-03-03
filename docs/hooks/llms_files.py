"""
MkDocs hook: copy llms reference files and swagger docs into the built site.

Serves raw files at:
  /llms.md
  /llms-go-sdk.md
  /llms-swift-sdk.md
  /swagger.json
  /swagger.yaml
  /openapi.json   (alias for swagger.json — same Swagger 2.0 content)

Generates interactive Swagger UI at:
  /api-reference/index.html
"""
import shutil
from pathlib import Path

SWAGGER_UI_HTML = """\
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Emergent API Reference</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    body {{ margin: 0; padding: 0; }}
    #swagger-ui .topbar {{ background-color: #3f51b5; }}
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({{
      url: "{base_url}swagger.json",
      dom_id: "#swagger-ui",
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "BaseLayout",
      deepLinking: true,
      defaultModelsExpandDepth: 1,
      defaultModelExpandDepth: 1,
      docExpansion: "list",
      filter: true,
      tryItOutEnabled: false,
    }});
  </script>
</body>
</html>
"""


def on_post_build(config):
    site_dir = Path(config["site_dir"])
    # docs_dir is docs/site; parent is docs/; parent.parent is repo root
    docs_dir = Path(config["docs_dir"])
    repo_root = docs_dir.parent.parent

    # --- llms reference files ---
    llms_files = [
        "docs/llms.md",
        "docs/llms-go-sdk.md",
        "docs/llms-swift-sdk.md",
    ]
    for rel_path in llms_files:
        src = repo_root / rel_path
        dst = site_dir / Path(rel_path).name
        if src.exists():
            shutil.copy2(src, dst)
            print(f"mkdocs-hook: copied {src.name} → {dst}")
        else:
            print(f"mkdocs-hook: WARNING {src} not found, skipping")

    # --- swagger spec files (swagger.json, swagger.yaml, openapi.json alias) ---
    swagger_src = repo_root / "docs/swagger/swagger.json"
    swagger_yaml_src = repo_root / "docs/swagger/swagger.yaml"

    for src, dst_name in [
        (swagger_src, "swagger.json"),
        (swagger_yaml_src, "swagger.yaml"),
        (swagger_src, "openapi.json"),   # alias — same Swagger 2.0 content
    ]:
        dst = site_dir / dst_name
        if src.exists():
            shutil.copy2(src, dst)
            print(f"mkdocs-hook: copied {src.name} → {dst_name}")
        else:
            print(f"mkdocs-hook: WARNING {src} not found, skipping")

    # --- Swagger UI page ---
    site_url = config.get("site_url", "") or ""
    if site_url and not site_url.endswith("/"):
        site_url += "/"
    # base_url is absolute so the swagger UI can find swagger.json from any subpath
    swagger_ui_dir = site_dir / "api-reference"
    swagger_ui_dir.mkdir(exist_ok=True)
    html = SWAGGER_UI_HTML.format(base_url=site_url)
    (swagger_ui_dir / "index.html").write_text(html, encoding="utf-8")
    print(f"mkdocs-hook: generated Swagger UI → {swagger_ui_dir}/index.html")
