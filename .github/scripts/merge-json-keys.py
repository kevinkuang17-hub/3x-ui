#!/usr/bin/env python3
# Git merge driver for locale JSON: merges key by key, so both sides adding keys never conflicts.
# Usage (set up by sync-upstream.yml): merge-json-keys.py BASE OURS THEIRS; the result replaces OURS.
import collections
import json
import os
import sys

MISSING = object()
conflicts = []
# Set by sync-upstream.yml on its second attempt: a string both sides changed takes upstream's text.
PREFER_THEIRS = os.environ.get("MERGE_PREFER_THEIRS") == "1"


def load(path):
    try:
        with open(path, encoding="utf-8") as f:
            text = f.read()
    except FileNotFoundError:
        return collections.OrderedDict()
    return json.loads(text, object_pairs_hook=collections.OrderedDict) if text.strip() else collections.OrderedDict()


def merge(base, ours, theirs, path):
    if not (isinstance(ours, dict) and isinstance(theirs, dict)):
        if theirs == base:
            return ours
        if ours == base or ours == theirs or PREFER_THEIRS:
            return theirs
        conflicts.append(path)
        return ours
    base = base if isinstance(base, dict) else collections.OrderedDict()
    keys = list(ours.keys())
    theirs_keys = list(theirs.keys())
    for i, k in enumerate(theirs_keys):
        if k in ours or k in base:
            continue
        prev = next((theirs_keys[j] for j in range(i - 1, -1, -1) if theirs_keys[j] in keys), None)
        keys.insert(keys.index(prev) + 1 if prev is not None else len(keys), k)
    out = collections.OrderedDict()
    for k in keys:
        b, o, t = base.get(k, MISSING), ours.get(k, MISSING), theirs.get(k, MISSING)
        if o is MISSING:
            if b is MISSING:
                out[k] = t
            elif t != b:
                if PREFER_THEIRS:
                    out[k] = t
                else:
                    conflicts.append(path + "." + k)
            continue
        if t is MISSING:
            if b is MISSING or o != b:
                out[k] = o
            continue
        out[k] = merge(b, o, t, path + "." + k)
    return out


def main():
    base_path, ours_path, theirs_path = sys.argv[1:4]
    try:
        result = merge(load(base_path), load(ours_path), load(theirs_path), "")
    except ValueError as err:
        print(f"merge-json-keys: {ours_path}: {err}", file=sys.stderr)
        return 1
    if conflicts:
        print("merge-json-keys: both sides changed " + ", ".join(conflicts), file=sys.stderr)
        return 1
    with open(ours_path, "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=2)
        f.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
