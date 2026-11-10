#!/usr/bin/env python3
"""Пересобирает manifest.json по содержимому папок rules/ и app/.

Запуск из корня репозитория:
    python3 tools/make_manifest.py --app-version 1.1.0 --notes "что нового"
Без --app-version берётся версия из текущего manifest.json.
"""
import argparse
import hashlib
import json
import os

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def sha(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


def title_of(path):
    with open(path, encoding="utf-8-sig") as f:
        for line in f:
            line = line.strip()
            if line == "---":
                break
            if line.lower().startswith("название:"):
                return line.split(":", 1)[1].strip()
    return ""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--app-version")
    ap.add_argument("--notes")
    a = ap.parse_args()

    old = {}
    mpath = os.path.join(ROOT, "manifest.json")
    if os.path.exists(mpath):
        with open(mpath, encoding="utf-8") as f:
            old = json.load(f)

    rules = []
    rdir = os.path.join(ROOT, "rules")
    for name in sorted(os.listdir(rdir)):
        if name.lower().endswith(".txt"):
            p = os.path.join(rdir, name)
            rules.append({"name": name, "title": title_of(p), "sha256": sha(p)})

    app_old = old.get("app", {})
    exe = "NovaHelper.exe"
    app = {
        "version": a.app_version or app_old.get("version", "1.0.0"),
        "file": exe,
        "sha256": sha(os.path.join(ROOT, "app", exe)),
        "notes": a.notes if a.notes is not None else app_old.get("notes", ""),
    }
    with open(mpath, "w", encoding="utf-8") as f:
        json.dump({"rules": rules, "app": app}, f, ensure_ascii=False, indent=2)
        f.write("\n")
    print(json.dumps({"rules": [r["name"] for r in rules], "app": app["version"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
