#!/usr/bin/env python3
"""Audit the SM Cloud backup against the local Station Manager database.

Fetches the tenant's full export from the smcloud service (GET /v1/export,
credentials read from the daemon config's smcloud forwarder entry) and
deep-compares it against the local sqlite ground truth. Read-only on both
sides (DB opened mode=ro, safe against a running daemon). Checks:

  - UUID parity: every live local QSO exists in the cloud and vice versa;
    tombstone counts reported
  - core columns match the stored payload field-for-field (freq compared
    through the kHz-column -> ADIF-MHz-string conversion)
  - every additional_data key matches the payload verbatim (the payload is
    the types.Qso JSON = core columns UNION the blob, so this proves
    full-fidelity round-trip)
  - the reconcile modified_at contract: cloud modified_at ==
    coalesce(local modified_at, created_at) truncated to seconds
  - field-population stats across the cloud payloads (gaps here mirror
    local data gaps -- the audit only FAILs on local/cloud disagreement)

Exit code: 0 = clean, 1 = at least one mismatch, 2 = usage/fetch error.

Usage:
  scripts/smcloud-audit.py                  # dogfood DB + config defaults
  scripts/smcloud-audit.py --db /path/to/station-manager.db \
                           --config /path/to/config.json
  scripts/smcloud-audit.py --export dump.json   # audit a saved export instead
"""

import argparse
import json
import sqlite3
import sys
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

DEFAULT_DB = Path.home() / ".local/share/station-manager/db/station-manager.db"
DEFAULT_CONFIG = Path.home() / ".local/share/station-manager/config.json"

CORE = [
    "call", "band", "mode", "freq", "qso_date", "time_on", "time_off",
    "rst_sent", "rst_rcvd", "country", "dedupe_key", "logbook_id",
]

POPULATION_FIELDS = [
    "call", "qso_date", "time_on", "band", "mode", "freq", "rst_sent",
    "rst_rcvd", "country", "dxcc", "cqz", "ituz", "cont", "gridsquare",
    "name", "lat", "lon", "distance", "my_gridsquare", "operator",
    "station_callsign", "my_rig",
]


def fetch_export(config_path: Path) -> dict:
    cfg = json.loads(config_path.read_text())
    fwd = next(
        (f for f in cfg.get("forwarders", []) if f.get("type") == "smcloud"), None
    )
    if fwd is None:
        sys.exit("no smcloud forwarder entry in " + str(config_path))
    creds = fwd.get("credentials", {})
    url = creds.get("url", "").rstrip("/") + "/v1/export"
    req = urllib.request.Request(
        url, headers={"Authorization": "Bearer " + creds.get("token", "")}
    )
    with urllib.request.urlopen(req, timeout=60) as resp:
        return json.load(resp)


def norm(v) -> str:
    return "" if v is None else str(v).strip()


def freq_mhz(khz) -> str:
    return f"{khz / 1000:.6f}".rstrip("0").rstrip(".")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--db", type=Path, default=DEFAULT_DB)
    ap.add_argument("--config", type=Path, default=DEFAULT_CONFIG)
    ap.add_argument("--export", type=Path, default=None,
                    help="audit a saved /v1/export JSON instead of fetching")
    ap.add_argument("--logbook", type=int, default=1, help="local logbook id")
    args = ap.parse_args()

    try:
        export = (
            json.loads(args.export.read_text())
            if args.export is not None
            else fetch_export(args.config)
        )
    except Exception as e:  # noqa: BLE001 -- report any fetch/parse failure as usage
        print("export fetch failed:", e, file=sys.stderr)
        return 2

    cloud, cloud_tombs = {}, 0
    for rec in export["qsos"]:
        if rec.get("deleted_at"):
            cloud_tombs += 1
        else:
            cloud[rec["uuid"]] = rec

    db = sqlite3.connect(f"file:{args.db}?mode=ro", uri=True)
    db.row_factory = sqlite3.Row
    local = {
        r["uuid"]: r
        for r in db.execute(
            "select * from qso where deleted_at is null and logbook_id = ?",
            (args.logbook,),
        )
    }
    local_tombs = db.execute(
        "select count(*) from qso where deleted_at is not null and logbook_id = ?",
        (args.logbook,),
    ).fetchone()[0]

    print(f"local live: {len(local)}  · local tombstones: {local_tombs}")
    print(f"cloud live: {len(cloud)}  · cloud tombstones: {cloud_tombs}")

    missing = set(local) - set(cloud)
    extra = set(cloud) - set(local)
    print(f"UUIDs missing in cloud: {len(missing)}  · unexpected in cloud: {len(extra)}")
    for u in sorted(missing)[:5]:
        print("   missing:", u, local[u]["call"], local[u]["qso_date"])
    for u in sorted(extra)[:5]:
        print("   extra:  ", u)

    core_bad, blob_bad, ts_bad = [], [], []
    for u, row in local.items():
        rec = cloud.get(u)
        if rec is None:
            continue
        p = rec["qso"]
        for c in CORE:
            lv = freq_mhz(row[c]) if c == "freq" else norm(row[c])
            pv = norm(p.get(c)).rstrip("0").rstrip(".") if c == "freq" else norm(p.get(c))
            if lv != pv:
                core_bad.append((u, c, row[c], p.get(c)))
        for k, v in json.loads(row["additional_data"] or "{}").items():
            if norm(v) != norm(p.get(k)):
                blob_bad.append((u, k, v, p.get(k)))
        lm = row["modified_at"] or row["created_at"]
        lm_dt = datetime.fromisoformat(lm.replace(" ", "T")).replace(tzinfo=timezone.utc)
        cm_dt = datetime.fromisoformat(rec["modified_at"]).astimezone(timezone.utc)
        if int(lm_dt.timestamp()) != int(cm_dt.replace(microsecond=0).timestamp()):
            ts_bad.append((u, lm, rec["modified_at"]))

    print(f"\ncore-column mismatches: {len(core_bad)}")
    for m in core_bad[:8]:
        print("   ", m)
    print(f"additional_data mismatches: {len(blob_bad)}")
    for m in blob_bad[:8]:
        print("   ", m)
    print(f"modified_at contract violations: {len(ts_bad)}")
    for m in ts_bad[:5]:
        print("   ", m)

    if cloud:
        print(f"\nfield population across {len(cloud)} cloud payloads:")
        for f in POPULATION_FIELDS:
            n = sum(1 for r in cloud.values() if norm(r["qso"].get(f)) != "")
            print(f"   {f:26s} {n:5d}  ({100 * n / len(cloud):5.1f}%)")

    clean = not (core_bad or blob_bad or ts_bad or missing or extra)
    print("\nRESULT:", "CLEAN — cloud copy is field-faithful" if clean else "MISMATCHES FOUND")
    return 0 if clean else 1


if __name__ == "__main__":
    sys.exit(main())
