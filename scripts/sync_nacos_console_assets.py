#!/usr/bin/env python3
"""从远程 Nacos 控制台同步 /next 静态资源到 assets/nacos-console/。

用法:
  CONSOLE_URL=http://host:8080/next python3 scripts/sync_nacos_console_assets.py
"""
from __future__ import annotations

import os
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[1]
OUT = Path(os.environ.get("NACOS_CONSOLE_STATIC_DIR", str(ROOT / "assets" / "nacos-console")))
CONSOLE_URL = os.environ.get("CONSOLE_URL", "http://10.100.113.151:8080/next").rstrip("/")

UA = "auto-dba-nacos-static-sync/1.0"


def fetch(url: str, timeout: int = 120) -> bytes:
    req = urllib.request.Request(url, headers={"User-Agent": UA, "Accept": "*/*"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.read()


def normalize(p: str) -> str:
    p = p.lstrip("./")
    if not p or p.startswith("http"):
        return ""
    if "/" not in p and p.endswith(".js"):
        return "js/" + p
    if "/" not in p and p.endswith(".css"):
        return "css/" + p
    return p


def main() -> int:
    u = urlparse(CONSOLE_URL)
    if not u.scheme or not u.netloc:
        print("invalid CONSOLE_URL", CONSOLE_URL, file=sys.stderr)
        return 2
    origin = f"{u.scheme}://{u.netloc}"
    console_path = (u.path.rstrip("/") or "/next")
    base = origin + console_path
    dest_root = OUT / console_path.lstrip("/")
    dest_root.mkdir(parents=True, exist_ok=True)

    print(f"sync nacos console static\n  source: {base}\n  dest:   {dest_root}")

    html = fetch(base + "/").decode("utf-8", "replace")
    (dest_root / "index.html").write_text(html, encoding="utf-8")

    seed = set()
    for m in re.finditer(r"""(?:src|href)=["'](\./[^"']+)["']""", html):
        seed.add(m.group(1)[2:].lstrip("/"))
    for extra in (
        "js/vendor-monaco.js",
        "js/vendor-markdown.js",
        "js/vendor-react.js",
        "js/vendor-icons.js",
        "js/vendor-ui.js",
        "css/main.css",
        "css/main2.css",
        "css/main3.css",
        "img/nacos-icon.png",
        "img/nacos-logo.svg",
        "img/nacos-logo-dark.svg",
    ):
        seed.add(extra)

    asset_re = re.compile(
        r"""["'`]((?:\./)?(?:js|css|img|assets|fonts)/[^"'`]+?\.(?:js|css|png|svg|jpg|jpeg|gif|woff2?|ttf|map))["'`]"""
    )
    import_re = re.compile(r"""import\(["'`]([^"'`]+)["'`]\)""")
    rel_re = re.compile(
        r"""["'`](\./[A-Za-z0-9_.@/-]+\.(?:js|css|png|svg|jpg|jpeg|gif|woff2?|ttf|map))["'`]"""
    )

    seen = set(seed)
    queue = list(seed)
    ok, fail = 0, 0
    static_ext = (".js", ".css", ".png", ".svg", ".jpg", ".jpeg", ".gif", ".woff", ".woff2", ".ttf", ".map")

    while queue:
        rel = queue.pop()
        try:
            data = fetch(base + "/" + rel)
        except Exception as e:
            print(f"  FAIL {rel}: {e}", file=sys.stderr)
            fail += 1
            continue
        path = dest_root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        ok += 1
        if not rel.endswith((".js", ".css", ".html", ".map")):
            print(f"  ok {rel} ({len(data)} bytes)")
            continue
        text = data.decode("utf-8", "replace")
        refs = set()
        for rx in (asset_re, import_re, rel_re):
            for m in rx.finditer(text):
                n = normalize(m.group(1))
                if n:
                    refs.add(n)
        added = 0
        for r in refs:
            if r not in seen and r.endswith(static_ext):
                seen.add(r)
                queue.append(r)
                added += 1
        print(f"  ok {rel} ({len(data)} bytes) +{added} new")

    (OUT / "VERSION").write_text(
        f"SOURCE_URL={base}\nCONSOLE_PATH={console_path}\nFILES_OK={ok}\nFILES_FAIL={fail}\n",
        encoding="utf-8",
    )
    print(f"done ok={ok} fail={fail} -> {dest_root}")
    return 0 if ok > 0 else 1


if __name__ == "__main__":
    sys.exit(main())
