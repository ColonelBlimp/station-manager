#!/usr/bin/env python3
"""UK/EI DX Contest Cabrillo from a Station Manager ADIF export (W-0023 weekend slice).

A standalone converter, outside the product: export the contest logbook as ADIF
from the Logbook view, then

  python3 -I scripts/ukeicc-cabrillo.py --from-daemon http://127.0.0.1:8080 \\
      -o 7Q5MLV.cbr --contest UKEIDXSSB --callsign 7Q5MLV --start 2026-10-31T12:00 \\
      --power LOW --assisted NON-ASSISTED --time 24-HOURS

--from-daemon reads every contact of the logbook straight from the running
daemon (GET /v1/logbook/{id}/qso, all pages; the daemon's default logbook
unless --logbook names another), so the log does not depend on which browser
tab or session logged what. Read-only. Instead, LOG.adi reads an ADIF export.

A temporary tool for the 2026 UK/EI DX SSB event: a Cabrillo export in the
product replaces it (W-0023).

Rules it applies (docs/work/W-0023-contest-logging.md, organiser-reply rulings):
  - Members are the records whose CONTEST_ID is the contest and whose
    APP_SM_CONTEST_START is the --start date (the occurrence: a dry run or
    another year's edition under the same CONTEST_ID is listed, not exported).
    Eligibility and the QSO: time use completion, QSO_DATE_OFF/TIME_OFF
    (ruling C), in the window [start, start+24h); the time is truncated to the
    minute. A member of this occurrence completed outside the window stops the
    export (it cannot be submitted, and must not vanish silently).
  - QSO: lines in order of serial sent (numeric). Duplicate contacts and
    duplicate serials are kept and listed (ruling A) — a station worked twice
    on a band, and a serial sent twice; gaps are listed.
  - Serials three digits minimum, 1000 onward unpadded (ruling B). A received
    serial typed as 0 is the explicit "not copied" record and exports as 000; a
    blank one stops the export.
  - The district received is SRX_STRING (ruling D): two letters, or -- typed as
    the explicit "not copied" record. A blank district stops the export for a
    UK/EI callsign; a DX station has none and exports --. Our own is -- (a DX
    entrant).
  - Mode must be the event's: SSB (USB/LSB) for the SSB event, CW for the CW
    one — never AM or FM. Bands 80/40/20/15/10 m, BAND and FREQ agreeing;
    frequency in kHz, rounded. An SSB contact outside the sponsor's required
    segments on 80/20 m is listed: rule 9.2 disallows all QSOs for operating
    outside them.

ERROR lines (a record or the file needs fixing) stop the export: the -o file
is not written or replaced (an earlier export stays as it was) and the exit
status is 1, so an incomplete file is never submitted by accident. The file is
written to a temporary name and renamed into place only on success. NOTE lines
are listed for checking and do not stop it.
"""
import argparse
import datetime as dt
import os
import re
import sys
import json
import tempfile
import urllib.error
import urllib.request

SSB_SUBMODES = {"", "USB", "LSB"}
BANDS = {"80m": (3500, 3800), "40m": (7000, 7200), "20m": (14000, 14350), "15m": (21000, 21450),
         "10m": (28000, 29700)}
# The sponsor's required SSB segments, rule 9.2 (as read 2026-10-28; to be re-checked against the page).
SSB_SEGMENTS = {"80m": [(3600, 3650), (3700, 3800)], "20m": [(14125, 14300)]}
UKEI_PREFIX = re.compile(r"^(?:[GM]|2[DEIJMUW]|E[IJ])[A-Z]?\d")
SERIAL = re.compile(r"^\d{1,5}$")
DISTRICT = re.compile(r"^(?:[A-Z]{2}|--)$")


class AdifError(Exception):
    pass


def parse_adif(data: bytes):
    """Records as {FIELD: value} dicts; field names upper case, lengths in bytes.
    A truncated value, a tag cut short or a record without its <EOR> is an
    AdifError: a contact must never vanish silently."""
    eoh = re.search(rb"<eoh>", data, re.I)
    i = eoh.end() if eoh else 0
    records, cur = [], {}
    tag = re.compile(rb"<([A-Za-z0-9_]+)(?::(\d+)(?::[A-Za-z])?)?>")
    while True:
        m = tag.search(data, i)
        if not m:
            break
        name = m.group(1).decode().upper()
        if name == "EOR":
            records.append(cur)
            cur = {}
            i = m.end()
            continue
        n = int(m.group(2) or 0)
        if m.end() + n > len(data):
            raise AdifError(f"ADIF field {name} is cut short (record {len(records) + 1})")
        cur[name] = data[m.end():m.end() + n].decode("utf-8", "replace")
        i = m.end() + n
    if b"<" in data[i:]:
        raise AdifError(f"ADIF ends inside a tag after record {len(records)}: the file is incomplete")
    if cur:
        raise AdifError(f"ADIF record {len(records) + 1} ({cur.get('CALL', '?')}) has no <EOR>: the file is incomplete")
    return records


def completion(r):
    """The UTC completion time (ruling C), or None when it is not recorded."""
    date = r.get("QSO_DATE_OFF") or r.get("QSO_DATE", "")
    time = r.get("TIME_OFF", "")
    if not (re.fullmatch(r"\d{8}", date) and re.fullmatch(r"\d{4}(\d{2})?", time)):
        return None
    return dt.datetime.strptime(date + time[:4] + (time[4:6] or "00"), "%Y%m%d%H%M%S")


def fmt_serial(n):
    return f"{n:03d}"


def khz(r):
    try:
        return round(float(r["FREQ"]) * 1000)
    except (KeyError, ValueError):
        return None


def check(r, a, errors, notes):
    """The record's QSO: fields, or None when it has an ERROR (appended)."""
    call = r.get("CALL", "").upper()
    where = f"{call or '?'} (sent {r.get('STX') or '?'})"
    bad = []
    stx, srx = r.get("STX", "").strip(), r.get("SRX", "").strip()
    district = r.get("SRX_STRING", "").strip().upper()
    if not SERIAL.match(stx):
        bad.append(f"serial sent (STX) {stx!r} is not a number")
    if srx == "":
        bad.append("no serial received: type it, or 0 if it was not copied")
    elif not SERIAL.match(srx):
        bad.append(f"serial received {srx!r} is not a number")
    if district == "" and UKEI_PREFIX.match(call):
        bad.append("UK/EI station with no district: type it, or -- if it was not copied")
    elif district != "" and not DISTRICT.match(district):
        bad.append(f"district {r.get('SRX_STRING')!r} is not two letters or --")
    mode, submode = r.get("MODE", "").upper(), r.get("SUBMODE", "").upper()
    want_cw = a.contest.endswith("CW")
    ok_mode = mode == "CW" if want_cw else (mode == "SSB" and submode in SSB_SUBMODES)
    if not ok_mode:
        bad.append(f"mode {mode or '?'}{'/' + submode if submode else ''} is not this event's")
    band, freq = r.get("BAND", "").lower(), khz(r)
    if band not in BANDS:
        bad.append(f"band {band or '?'} is not a contest band")
    elif freq is None or not (BANDS[band][0] <= freq <= BANDS[band][1]):
        bad.append(f"frequency {r.get('FREQ')!r} is not on {band}")
    elif not want_cw and band in SSB_SEGMENTS and not any(lo <= freq <= hi for lo, hi in SSB_SEGMENTS[band]):
        notes.append(f"{where}: {freq} kHz is outside the sponsor's required SSB segments on {band}; "
                     "rule 9.2 disallows all QSOs for operating outside them — check")
    if not re.fullmatch(r"[A-Z0-9/]{3,}", call):
        bad.append(f"callsign {call!r} is malformed")
    if bad:
        errors.extend(f"{where}: {b}" for b in bad)
        return None
    return (int(stx), str(freq), "CW" if want_cw else "PH", call, band, r.get("RST_SENT", "59"),
            r.get("RST_RCVD", "59"), int(srx), district or "--")


def convert(records, a):
    """(header+QSO lines, errors, notes)."""
    errors, notes, rows = [], [], []
    start = dt.datetime.fromisoformat(a.start)
    end = start + dt.timedelta(hours=24)
    occurrence = start.strftime("%Y%m%d")
    for r in records:
        if r.get("CONTEST_ID", "").upper() != a.contest:
            continue
        if r.get("APP_SM_CONTEST_START", "") != occurrence:
            notes.append(f"{r.get('CALL', '?')}: {a.contest} of {r.get('APP_SM_CONTEST_START') or 'no recorded date'}, "
                         "not this event — left out")
            continue
        when = completion(r)
        if when is None:
            errors.append(f"{r.get('CALL', '?')}: no completion time (QSO_DATE_OFF/TIME_OFF)")
            continue
        if not (start <= when < end):
            errors.append(f"{r.get('CALL', '?')}: logged for this event but completed {when:%Y-%m-%d %H:%M:%S}Z, "
                          "outside its window: correct the time, or remove the contest fields if it is not a contest QSO")
            continue
        row = check(r, a, errors, notes)
        if row is not None:
            rows.append((row[0], when) + row[1:])

    rows.sort(key=lambda x: (x[0], x[1]))
    seen, worked = {}, {}
    for row in rows:
        seen.setdefault(row[0], []).append(row[4])
        worked.setdefault((row[4], row[5]), []).append(row[0])
    for stx, calls in seen.items():
        if len(calls) > 1:
            notes.append(f"serial {fmt_serial(stx)} sent more than once: {', '.join(calls)} — all kept")
    for (call, band), serials in worked.items():
        if len(serials) > 1:
            notes.append(f"{call} worked more than once on {band} (sent {', '.join(map(fmt_serial, serials))})"
                         " — all kept")
    if rows:
        missing = sorted(set(range(1, rows[-1][0] + 1)) - set(seen))
        if missing:
            notes.append(f"unused serials (gaps, expected): {', '.join(map(fmt_serial, missing))}")

    out = [
        "START-OF-LOG: 3.0",
        f"CONTEST: {a.contest}",
        f"CALLSIGN: {a.callsign}",
        "CATEGORY-OPERATOR: SINGLE-OP",
        f"CATEGORY-ASSISTED: {a.assisted}",
        f"CATEGORY-POWER: {a.power}",
        f"CATEGORY-TIME: {a.time}",
        f"CATEGORY-MODE: {'CW' if a.contest.endswith('CW') else 'SSB'}",
        "CREATED-BY: Station Manager ukeicc-cabrillo.py",
    ]
    for stx, when, freq, mode, call, _band, rsts, rstr, srx, district in rows:
        out.append(
            f"QSO: {freq:>5} {mode} {when:%Y-%m-%d %H%M} {a.callsign:<13} {rsts:>3} {fmt_serial(stx)} -- "
            f"{call:<13} {rstr:>3} {fmt_serial(srx)} {district}"
        )
    out.append("END-OF-LOG:")
    return out, errors, notes


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    p.add_argument("adif", nargs="?", help="an ADIF export (or use --from-daemon)")
    p.add_argument("--from-daemon", metavar="URL", help="read the contacts from the running daemon, e.g. http://127.0.0.1:8080")
    p.add_argument("--logbook", type=int, help="with --from-daemon: the logbook id (default: the daemon's default logbook)")
    p.add_argument("-o", "--output", required=True, help="the Cabrillo file; replaced only on success")
    p.add_argument("--contest", required=True, type=str.upper, choices=["UKEIDXSSB", "UKEIDXCW"])
    p.add_argument("--callsign", required=True, type=str.upper)
    p.add_argument("--start", required=True, help="contest start, UTC, e.g. 2026-10-31T12:00")
    p.add_argument("--power", required=True, choices=["HIGH", "LOW", "QRP"])
    p.add_argument("--assisted", required=True, choices=["ASSISTED", "NON-ASSISTED"])
    p.add_argument("--time", required=True, choices=["24-HOURS", "12-HOURS"])
    a = p.parse_args(argv)
    if (a.adif is None) == (a.from_daemon is None):
        p.error("give an ADIF file or --from-daemon URL, not both")
    try:
        if a.from_daemon:
            records = fetch_daemon(a.from_daemon, a.logbook)
        else:
            with open(a.adif, "rb") as f:
                records = parse_adif(f.read())
    except (urllib.error.URLError, OSError, ValueError) as e:
        print(f"ERROR: could not read the contacts: {e}", file=sys.stderr)
        print("no Cabrillo written", file=sys.stderr)
        return 1
    except AdifError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        print("no Cabrillo written", file=sys.stderr)
        return 1
    lines, errors, notes = convert(records, a)
    for line in notes:
        print(f"NOTE: {line}", file=sys.stderr)
    for line in errors:
        print(f"ERROR: {line}", file=sys.stderr)
    if errors:
        print(f"{len(errors)} error(s): fix them in the logbook, export again — no Cabrillo written", file=sys.stderr)
        return 1
    write_atomically(a.output, "\n".join(lines) + "\n")
    print(f"{a.output}: {sum(l.startswith('QSO:') for l in lines)} QSO line(s); {len(notes)} note(s)", file=sys.stderr)
    return 0


def _get(base, path):
    with urllib.request.urlopen(base.rstrip("/") + path, timeout=30) as r:
        return json.load(r)


def fetch_daemon(base, logbook=None):
    """Every QSO of the logbook as {ADIF FIELD: value} records, from the daemon's
    paged list (the default logbook unless one is named). Read-only."""
    if logbook is None:
        default = (_get(base, "/v1/version").get("archive") or {}).get("default_logbook_uuid")
        books = [b for b in _get(base, "/v1/logbook") if b.get("uuid") == default]
        if not default or len(books) != 1:
            raise AdifError("the daemon reports no default logbook; name one with --logbook")
        logbook = books[0]["id"]
    records, after = [], None
    while True:
        path = f"/v1/logbook/{int(logbook)}/qso?limit=500" + (f"&after={after}" if after else "")
        page = _get(base, path)
        for item in page.get("items") or []:
            records.append({k.upper(): str(v) for k, v in item.items()
                            if v is not None and isinstance(v, (str, int, float))})
        after = page.get("next_cursor")
        if not after:
            return records


def write_atomically(path, text):
    """Write to a temporary file beside path, then rename it into place."""
    fd, tmp = tempfile.mkstemp(prefix=".cabrillo-", dir=os.path.dirname(os.path.abspath(path)))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(text)
        os.replace(tmp, path)
    except BaseException:
        os.unlink(tmp)
        raise


if __name__ == "__main__":
    sys.exit(main())
