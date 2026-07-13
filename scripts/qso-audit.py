#!/usr/bin/env python3
"""Audit recent QSOs in a Station Manager database for consistency + completeness.

Read-only (opens the DB with mode=ro, safe against a running daemon). Checks the
last N QSOs (default 90) for:

  - core column sanity (call/band/mode/freq/country present, country not Unknown)
  - band <-> freq agreement (ADIF band edges, freq stored in kHz)
  - additional_data JSON parses and its mirror keys match the columns
  - enrichment injected (dxcc/cqz/ituz/cont/gridsquare/country_details)
  - station-side identity injected (my_gridsquare/my_dxcc/station_callsign/...)
  - best-effort QRZ detail present (name/lat/lon/qth/email) -- warnings only,
    a QRZ miss must never have blocked logging
  - upload queue: every QSO has forwarder rows, none failed/stuck
  - duplicate dedupe_keys inside the window
  - time_on <= time_off and qso_date == created_at UTC date (warnings; both
    legitimately differ across midnight)

Exit code: 0 = clean (warnings allowed), 1 = at least one FAIL, 2 = usage error.

Usage:
  scripts/qso-audit.py                      # last 90 QSOs, default dogfood DB
  scripts/qso-audit.py --last 200 --mode FT8
  scripts/qso-audit.py --db /path/to/station-manager.db -v
"""

import argparse
import json
import sqlite3
import sys
from collections import Counter, defaultdict
from pathlib import Path

DEFAULT_DB = Path.home() / ".local/share/station-manager/db/station-manager.db"

# ADIF band edges in kHz (the qso.freq column is kHz). Widest envelopes -- the
# same region-agnostic posture as the SPA's frequencyToBand.
BAND_KHZ = {
    "160m": (1800, 2000),
    "80m": (3500, 4000),
    "60m": (5060, 5450),
    "40m": (7000, 7300),
    "30m": (10100, 10150),
    "20m": (14000, 14350),
    "17m": (18068, 18168),
    "15m": (21000, 21450),
    "12m": (24890, 24990),
    "10m": (28000, 29700),
    "6m": (50000, 54000),
    "2m": (144000, 148000),
    "70cm": (420000, 450000),
}

# additional_data keys that MIRROR a qso column -- they must agree. Compared as
# strings; a column that is empty may legitimately be absent from the JSON
# (omitempty), so absent == empty.
MIRROR_KEYS = [
    "uuid",
    "call",
    "band",
    "mode",
    "qso_date",
    "time_on",
    "time_off",
    "rst_sent",
    "rst_rcvd",
    "country",
    "dedupe_key",
]

# Enrichment/derived fields every stored QSO should carry (populate-data-ourselves:
# SM computes/stores these; missing = the enrichment injection failed or predates it).
ENRICH_REQUIRED = ["country", "cont", "cqz", "ituz", "dxcc", "gridsquare", "country_details"]

# Station-side identity stamped from the operator's config at submit time.
STATION_REQUIRED = [
    "station_callsign",
    "operator",
    "owner_callsign",
    "my_gridsquare",
    "my_dxcc",
    "my_cq_zone",
    "my_itu_zone",
    "my_country",
    "tx_pwr",
    "my_rig",
    "my_antenna",
]

# Best-effort QRZ profile detail -- absence is a WARN, never a FAIL (the
# enrichment-never-blocks-logging invariant means these can be legitimately empty).
QRZ_OPTIONAL = ["name", "lat", "lon", "qth", "email"]


def fetch(con, last, mode):
    q = "SELECT * FROM qso ORDER BY id DESC LIMIT ?"
    rows = [dict(r) for r in con.execute(q, (last,))]
    rows.reverse()  # oldest first reads naturally in the report
    if mode:
        rows = [r for r in rows if r["mode"].upper() == mode.upper()]
    return rows


def audit(con, rows, verbose):
    fails, warns = [], []

    def fail(r, msg):
        fails.append((r["id"], r["call"], msg))

    def warn(r, msg):
        warns.append((r["id"], r["call"], msg))

    ids = [r["id"] for r in rows]
    uploads = defaultdict(list)
    if ids:
        ph = ",".join("?" * len(ids))
        for u in con.execute(
            f"SELECT qso_id, forwarder_name, status, attempts, last_error "
            f"FROM qso_upload WHERE qso_id IN ({ph})",
            ids,
        ):
            uploads[u["qso_id"]].append(dict(u))

    dedupe_seen = Counter(r["dedupe_key"] for r in rows)

    for r in rows:
        # --- core columns ---
        if r["deleted_at"] is not None:
            warn(r, "soft-deleted row inside the window")
        for col in ("call", "band", "mode", "country"):
            if not str(r[col]).strip():
                fail(r, f"column {col} is empty")
        if r["country"].strip().lower() == "unknown":
            fail(r, "country is 'Unknown' (enrichment miss was stored)")
        if not r["freq"]:
            fail(r, "freq is 0/empty")
        if not str(r["rst_sent"]).strip():
            warn(r, "rst_sent empty")
        if not str(r["rst_rcvd"]).strip():
            warn(r, "rst_rcvd empty")

        # --- band <-> freq ---
        rng = BAND_KHZ.get(r["band"])
        if rng is None:
            warn(r, f"band {r['band']!r} not in the audit's band table")
        elif not (rng[0] <= r["freq"] <= rng[1]):
            fail(r, f"freq {r['freq']} kHz outside {r['band']} ({rng[0]}-{rng[1]})")

        # --- additional_data ---
        try:
            ad = json.loads(r["additional_data"])
        except (json.JSONDecodeError, TypeError) as e:
            fail(r, f"additional_data does not parse: {e}")
            continue

        for k in MIRROR_KEYS:
            col, mirror = str(r[k]).strip(), str(ad.get(k, "")).strip()
            if col != mirror:
                fail(r, f"additional_data.{k}={mirror!r} != column {col!r}")
        if str(ad.get("logbook_id", "")) != str(r["logbook_id"]):
            fail(r, f"additional_data.logbook_id={ad.get('logbook_id')!r} != {r['logbook_id']}")

        # freq mirror is MHz-as-string next to the kHz column
        ad_freq = ad.get("freq", "")
        try:
            if round(float(ad_freq) * 1000) != r["freq"]:
                fail(r, f"additional_data.freq={ad_freq!r} MHz != column {r['freq']} kHz")
        except ValueError:
            fail(r, f"additional_data.freq={ad_freq!r} is not numeric")

        for k in ENRICH_REQUIRED:
            if not str(ad.get(k, "")).strip():
                fail(r, f"enrichment field {k} missing/empty")
        dxcc = str(ad.get("dxcc", "")).strip()
        if dxcc and not dxcc.isdigit():
            fail(r, f"dxcc {dxcc!r} is not numeric")

        for k in STATION_REQUIRED:
            if not str(ad.get(k, "")).strip():
                fail(r, f"station field {k} missing/empty")

        missing_qrz = [k for k in QRZ_OPTIONAL if not str(ad.get(k, "")).strip()]
        if missing_qrz:
            warn(r, f"QRZ best-effort fields absent: {', '.join(missing_qrz)}")

        # --- times ---
        if r["time_off"] < r["time_on"]:
            warn(r, f"time_off {r['time_off']} < time_on {r['time_on']} (midnight wrap?)")
        created_date = str(r["created_at"])[:10].replace("-", "")
        if created_date != r["qso_date"]:
            warn(r, f"qso_date {r['qso_date']} != created_at date {created_date}")

        # --- upload queue ---
        ups = uploads.get(r["id"], [])
        if not ups:
            fail(r, "no qso_upload rows (one-fails-all-fail says these are atomic)")
        for u in ups:
            if u["status"] == "failed":
                fail(r, f"upload to {u['forwarder_name']} FAILED after {u['attempts']}: {u['last_error']}")
            elif u["status"] != "uploaded":
                warn(r, f"upload to {u['forwarder_name']} is {u['status']} (attempts={u['attempts']})")

        # --- dupes ---
        if dedupe_seen[r["dedupe_key"]] > 1:
            fail(r, "dedupe_key duplicated inside the window")

    return fails, warns


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--db", type=Path, default=DEFAULT_DB, help=f"database path (default {DEFAULT_DB})")
    p.add_argument("--last", type=int, default=90, help="how many most-recent QSOs to audit (default 90)")
    p.add_argument("--mode", help="only audit QSOs of this mode (e.g. FT8)")
    p.add_argument("-v", "--verbose", action="store_true", help="also list every audited QSO")
    args = p.parse_args()

    if not args.db.exists():
        print(f"error: no database at {args.db}", file=sys.stderr)
        return 2

    con = sqlite3.connect(f"file:{args.db}?mode=ro", uri=True)
    con.row_factory = sqlite3.Row

    rows = fetch(con, args.last, args.mode)
    if not rows:
        print("no QSOs matched")
        return 0

    fails, warns = audit(con, rows, args.verbose)

    modes = Counter(r["mode"] for r in rows)
    bands = Counter(r["band"] for r in rows)
    print(f"audited {len(rows)} QSOs  (ids {rows[0]['id']}..{rows[-1]['id']}, "
          f"{rows[0]['qso_date']}..{rows[-1]['qso_date']})")
    print(f"  modes: {dict(modes)}   bands: {dict(bands)}")

    if args.verbose:
        for r in rows:
            print(f"  #{r['id']} {r['qso_date']} {r['time_on']} {r['call']:<10} "
                  f"{r['band']:<4} {r['mode']:<4} {r['country']}")

    def dump(title, items):
        print(f"\n{title} ({len(items)}):")
        for qid, call, msg in items:
            print(f"  #{qid} {call}: {msg}")

    if fails:
        dump("FAIL", fails)
    if warns:
        dump("warnings", warns)
    if not fails and not warns:
        print("\nall checks passed — no findings")
    elif not fails:
        print("\nno hard failures — warnings only")

    return 1 if fails else 0


if __name__ == "__main__":
    sys.exit(main())
