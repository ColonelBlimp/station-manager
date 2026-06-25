#!/usr/bin/env python3
"""Generate internal/enums/dxcc/dxcc-entities.json.

Builds the hamnut-aligned DXCC reference table the new-entity check needs:
hamnut's `primaryDXCCPrefix` -> ADIF DXCC entity number. hamnut resolves a
callsign to an entity but only emits the alpha prefix (e.g. "UA" vs "UA9" for
European vs Asiatic Russia), never the numeric ADIF code; QSOs store the numeric
code. This table bridges the two so the orchestrator can match by entity number
instead of the (mismatching) country-name string.

Provenance / how it's generated (so it's reproducible — KISS, embed-for-now):
  - Seed = distinct (dxcc number, a sample callsign) pairs from a real log DB.
    The numeric DXCC on each QSO is authoritative (ADIF import / QRZ enrichment).
  - For each entity's sample call, query hamnut for its `primaryDXCCPrefix` +
    `countryName`, pairing hamnut's exact vocabulary with the authoritative number.

The output covers the entities present in the seed log (every entity an operator
has worked — i.e. every real new-entity false-positive case). Unmapped prefixes
fall back to name-matching at runtime, so partial coverage is safe; extend by
re-running against a richer log or via the operator override file.

Usage:
  scripts/gen-dxcc-entities.py [DB_PATH] > internal/enums/dxcc/dxcc-entities.json
Default DB_PATH: ~/.local/share/station-manager/db/station-manager.db
"""

import json
import os
import sqlite3
import sys
import time
import urllib.parse
import urllib.request

HAMNUT = "https://api.hamnut.com/v1/call-signs/prefixes"
DEFAULT_DB = os.path.expanduser(
    "~/.local/share/station-manager/db/station-manager.db"
)


def seed_from_log(db_path):
    """distinct dxcc number -> (sample callsign, stored country), from
    non-deleted QSOs. The stored country is kept so a prefix collision can be
    resolved by preferring the candidate whose stored name matches hamnut's
    (which rejects log misfiles — e.g. a Colombian HK4 call mistakenly logged as
    Dominican Republic)."""
    con = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    con.execute("PRAGMA query_only=ON")
    seed = {}
    for ad, call in con.execute(
        "SELECT additional_data, call FROM qso WHERE deleted_at IS NULL"
    ):
        try:
            d = json.loads(ad) if ad else {}
        except Exception:
            d = {}
        dx = str(d.get("dxcc", "")).strip()
        if dx and dx not in seed and call:
            seed[dx] = (call.strip(), str(d.get("country", "")).strip())
    con.close()
    return seed


def hamnut(call):
    url = f"{HAMNUT}?{urllib.parse.urlencode({'prefix': call})}"
    req = urllib.request.Request(url, headers={"User-Agent": "sm-dxcc-gen"})
    with urllib.request.urlopen(req, timeout=15) as r:
        return json.load(r)


def main():
    db_path = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_DB
    seed = seed_from_log(db_path)
    print(f"seed: {len(seed)} entities from {db_path}", file=sys.stderr)

    # Resolve each entity at hamnut, collecting candidates per primary prefix.
    # A prefix should be 1:1 with an entity; a collision means a log misfile (a
    # call whose stored dxcc disagrees with hamnut's resolution).
    candidates = {}  # prefix -> list of {dxcc, name, stored, namematch}
    for dxcc, (call, stored) in sorted(seed.items(), key=lambda kv: int(kv[0])):
        try:
            r = hamnut(call)
        except Exception as e:
            print(f"  skip dxcc={dxcc} ({call}): {e}", file=sys.stderr)
            continue
        if not r.get("found"):
            print(f"  skip dxcc={dxcc} ({call}): not found", file=sys.stderr)
            continue
        prefix = (r.get("primaryDXCCPrefix") or "").strip().upper()
        name = (r.get("countryName") or "").strip()
        if not prefix:
            print(f"  skip dxcc={dxcc} ({call}): no prefix", file=sys.stderr)
            continue
        candidates.setdefault(prefix, []).append({
            "dxcc": int(dxcc),
            "name": name,
            "namematch": stored.strip().lower() == name.lower(),
        })
        time.sleep(0.15)  # be polite to hamnut

    # Resolve collisions: prefer the candidate whose log-stored country name
    # matched hamnut's (rejects misfiles); otherwise the lowest dxcc.
    by_prefix = {}
    for prefix, cands in candidates.items():
        if len(cands) > 1:
            print(
                f"  collision {prefix}: "
                + ", ".join(f"{c['dxcc']}{'*' if c['namematch'] else ''}"
                            for c in cands),
                file=sys.stderr,
            )
        matched = [c for c in cands if c["namematch"]]
        pick = min(matched or cands, key=lambda c: c["dxcc"])
        by_prefix[prefix] = {"prefix": prefix, "dxcc": pick["dxcc"], "name": pick["name"]}

    entities = sorted(by_prefix.values(), key=lambda e: e["dxcc"])
    out = {
        "version": "1",
        "comment": (
            "hamnut primaryDXCCPrefix -> ADIF DXCC entity number. "
            "Generated by scripts/gen-dxcc-entities.py (see its docstring). "
            "Unmapped prefixes fall back to country-name matching at runtime."
        ),
        "entities": entities,
    }
    print(f"generated: {len(entities)} entities", file=sys.stderr)
    json.dump(out, sys.stdout, indent=2, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
