#!/usr/bin/env python3
"""Checks the translations in web/i18n against the German texts in the code.

German is the source language: JS uses tr("…")/trh("…") and data-i18n attributes,
Go uses L/errf/sprintf/errNew/j.logf/j.setStep. Prints missing, obsolete and broken
entries per language. `--missing-json` writes the missing keys to i18n-missing.json.
"""
import glob, html, json, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LIT = r'"(?:[^"\\\n]|\\.)*"'


def keys():
    out = {}
    def add(k):
        if re.search(r"[A-Za-zÄÖÜäöüß]{2}", re.sub(r"%[a-z]", "", k)):
            out.setdefault(k, True)
    js = open(os.path.join(ROOT, "web/app.js"), encoding="utf-8").read()
    for m in re.finditer(r"\btrh?\(\s*(" + LIT + ")", js):
        add(json.loads(m.group(1)))
    page = open(os.path.join(ROOT, "web/index.html"), encoding="utf-8").read()
    for m in re.finditer(r'data-i18n(?:-title)?="([^"]*)"', page):
        add(html.unescape(m.group(1)))
    for f in glob.glob(os.path.join(ROOT, "*.go")):
        if f.endswith("_test.go") or f.endswith("mock.go"):
            continue
        src = open(f, encoding="utf-8").read()
        for m in re.finditer(r"(?:\bL|\berrf|\bsprintf|\berrNew|\.logf|\.setStep|errorString)\(\s*(" + LIT + ")", src):
            add(json.loads(m.group(1)))
        if f.endswith("sets.go"):
            blk = src[src.index("var modSets"):src.index("// translatedSets")]
            for m in re.finditer(r'\{"\w+", "[^"]*", (' + LIT + "), (" + LIT + "),", blk):
                add(json.loads(m.group(1)))
                add(json.loads(m.group(2)))
    return list(out)


def main():
    ks = keys()
    bad = 0
    missing_all = {}
    for f in sorted(glob.glob(os.path.join(ROOT, "web/i18n/*.json"))):
        lang = os.path.basename(f)[:-5]
        d = json.load(open(f, encoding="utf-8"))
        missing = [k for k in ks if not d.get(k)]
        obsolete = [k for k in d if k not in ks]
        broken = []
        for k in ks:
            v = d.get(k)
            if not v:
                continue
            if sorted(re.findall(r"\{\d+\}", k)) != sorted(re.findall(r"\{\d+\}", v)) or \
               re.findall(r"%[-+#0-9.]*[a-zA-Z%]", k) != re.findall(r"%[-+#0-9.]*[a-zA-Z%]", v):
                broken.append(k)
        print(f"{lang:6} missing {len(missing):3}  obsolete {len(obsolete):3}  broken {len(broken):3}")
        for k in broken:
            print("   broken:", k)
        if missing:
            missing_all[lang] = missing
        bad += len(missing) + len(broken)
    if "--missing-json" in sys.argv:
        json.dump(missing_all, open("i18n-missing.json", "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print(f"{len(ks)} texts in the code")
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
