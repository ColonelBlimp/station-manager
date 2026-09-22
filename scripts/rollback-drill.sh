#!/usr/bin/env bash
# Paired rollback drill (W-0021, ruling (e) 2026-09-22): prove on DISPOSABLE
# copies that `smd db-downgrade` + `smd config-downgrade` let an OLDER build
# open the station's files, with every QSO / upload / history / logbook row
# and UUID identical to the copy before the drill.
#
# Nothing here touches the live files: they are copied into a 0700 scratch
# directory under $HOME, the config copy is rewritten so data_dir, the
# datastore path and the socket all live in the scratch (a bare working
# directory is NOT isolation — a non-empty data_dir wins over SM_WORKING_DIR
# and SQLite opens datastore.path as given), every external path is switched
# off (forwarders, bridge, FT8, evidence capture/sync, SMTP, PSK Reporter,
# lookup providers), and the script refuses to start any daemon unless every
# resolved database path lies under the scratch root. Credentials are removed
# before the scratch config is written; it is 0600 and shredded on exit.
#
#   scripts/rollback-drill.sh --new <smd> --new-schema N --new-config M \
#       --old <smd> --old-schema N --old-config M \
#       [--db <station-manager.db>] [--config <config.json>] [--label <name>] [--keep]
#       [--expect-dropped "table.col,table.col"]   # EXACT set of the SOURCE copy's columns that
#                                                  # the old schema lacks (12→11 drops nothing the
#                                                  # source had; 11→9 drops two qso_upload columns)
#       [--mutate-one-row]   # proof hook: changes one retained value so the verdict MUST fail
#
# The expected versions are asserted at every phase, so a drill whose new
# binary did not actually migrate (or whose downgrade step was skipped) fails
# instead of printing ROWS IDENTICAL for a no-op. The database copy is a
# consistent SQLite online backup, not a file copy, so a source daemon that is
# still writing cannot leave the copy torn. Credentials are scrubbed from the
# config before the scratch copy is written (every external path is off, so
# none is needed); nothing secret is ever on disk in the scratch.
set -euo pipefail

NEW=""; OLD=""; NEW_SCHEMA=""; NEW_CONFIG=""; OLD_SCHEMA=""; OLD_CONFIG=""; LABEL="old"; KEEP=0; MUTATE=0; EXPECT_DROPPED=""
DB="$HOME/.local/share/station-manager/db/station-manager.db"
CFG="$HOME/.local/share/station-manager/config.json"
while [ $# -gt 0 ]; do
  case "$1" in
    --new) NEW="$2"; shift 2;; --old) OLD="$2"; shift 2;;
    --new-schema) NEW_SCHEMA="$2"; shift 2;; --new-config) NEW_CONFIG="$2"; shift 2;;
    --old-schema) OLD_SCHEMA="$2"; shift 2;; --old-config) OLD_CONFIG="$2"; shift 2;;
    --db) DB="$2"; shift 2;; --config) CFG="$2"; shift 2;;
    --label) LABEL="$2"; shift 2;; --keep) KEEP=1; shift;;
    --mutate-one-row) MUTATE=1; shift;;
    --expect-dropped) EXPECT_DROPPED="$2"; shift 2;;
    *) echo "unknown argument: $1" >&2; exit 2;;
  esac
done
[ -n "$NEW" ] && [ -n "$OLD" ] && [ -n "$NEW_SCHEMA" ] && [ -n "$NEW_CONFIG" ] && [ -n "$OLD_SCHEMA" ] && [ -n "$OLD_CONFIG" ] || { echo "usage: --new --new-schema --new-config --old --old-schema --old-config required" >&2; exit 2; }
[ "$OLD_SCHEMA" -le "$NEW_SCHEMA" ] && [ "$OLD_CONFIG" -le "$NEW_CONFIG" ] || { echo "old versions must not exceed new versions" >&2; exit 2; }
[ -x "$NEW" ] && [ -x "$OLD" ] || { echo "binaries must be executable" >&2; exit 2; }
[ -r "$DB" ] && [ -r "$CFG" ] || { echo "source db/config not readable" >&2; exit 2; }

SCRATCH="$(mktemp -d "$HOME/.sm-rollback-drill.XXXXXX")"
chmod 700 "$SCRATCH"
PID=""
cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then kill -TERM "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; fi
  if [ "$KEEP" = 1 ]; then echo "kept scratch: $SCRATCH (config copy was scrubbed; remove it when done)"; return; fi
  # Belt and braces only: the scratch config never held a credential (scrubbed
  # before the write), and the daemon replaces it atomically, so shredding by
  # name cannot reach earlier inodes anyway.
  find "$SCRATCH" -name 'config.json*' -type f -exec shred -u {} \; 2>/dev/null || true
  rm -rf "$SCRATCH"
}
trap cleanup EXIT

echo "drill[$LABEL]: scratch $SCRATCH"
mkdir -p "$SCRATCH/db"
# A consistent snapshot even while the source daemon writes: SQLite's online
# backup API, not a file copy of db + WAL taken at two different instants.
python3 - "$DB" "$SCRATCH/db/station-manager.db" <<'PY'
import sqlite3, sys
src = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
dst = sqlite3.connect(sys.argv[2])
with dst: src.backup(dst)
dst.close(); src.close()
PY
chmod 600 "$SCRATCH"/db/*

# Rewrite the config copy into the scratch and switch every external path off.
python3 - "$CFG" "$SCRATCH" <<'PY'
import json, sys, os
src, scratch = sys.argv[1], sys.argv[2]
doc = json.load(open(src))
doc["data_dir"] = scratch
doc.setdefault("datastore", {})["path"] = os.path.join(scratch, "db", "station-manager.db")
doc.setdefault("server", {})["protocol"] = "unix"
doc["server"]["serve_spa"] = False
doc["socket_path"] = os.path.join(scratch, "smd.sock")
for f in doc.get("forwarders", []): f["enabled"] = False
doc.setdefault("bridge", {})["enabled"] = False
doc.setdefault("ft8", {})["enabled"] = False
ev = doc.setdefault("evidence", {}); ev["capture"] = False; ev["sync"] = False
doc.setdefault("smtp", {})["enabled"] = False
doc.setdefault("psk_reporter", {})["enabled"] = False
def off(node):
    if isinstance(node, dict):
        if "enabled" in node: node["enabled"] = False
        for v in node.values(): off(v)
    elif isinstance(node, list):
        for v in node: off(v)
off(doc.get("lookup", {}))
doc.setdefault("logging", {})["file_logging"] = True
# Credentials never reach the scratch: every external path is off, so none is
# needed, and a scrubbed file needs no shredding discipline.
SECRET_KEYS = {"password", "token", "api_key", "apikey", "secret", "bearer_token", "credentials"}
def scrub(node):
    if isinstance(node, dict):
        for k in list(node):
            if k.lower() in SECRET_KEYS:
                node[k] = {} if isinstance(node[k], dict) else ""
            else:
                scrub(node[k])
    elif isinstance(node, list):
        for v in node: scrub(v)
scrub(doc)
# The archive catalogue (config v4+): a legacy/external entry names a file by
# absolute path. The live file's path is mapped onto the scratch copy; any
# other path is refused rather than guessed.
src_db = os.path.realpath(json.load(open(src)).get("datastore", {}).get("path", ""))
for a in doc.get("qso_archives", []):
    p = a.get("path")
    if not p: continue
    if os.path.realpath(p) == src_db:
        a["path"] = doc["datastore"]["path"]
    else:
        print(f"REFUSING: qso_archives[{a.get('id')}].path {p} is not the live datastore; no mapping into the scratch"); sys.exit(3)
out = os.path.join(scratch, "config.json")
fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w") as fh: json.dump(doc, fh, indent=2)
# Path confinement: every resolved database path must lie under the scratch.
paths = {"data_dir": doc["data_dir"], "datastore.path": doc["datastore"]["path"],
         "reference.db (derived)": os.path.join(os.path.dirname(doc["datastore"]["path"]), "reference.db"),
         "evidence.db (derived)": os.path.join(os.path.dirname(doc["datastore"]["path"]), "evidence.db"),
         "backups (derived)": os.path.join(os.path.dirname(doc["datastore"]["path"]), "backups"),
         "socket": doc["socket_path"]}
for a in doc.get("qso_archives", []):
    if a.get("path"): paths[f"qso_archives[{a.get('id')}].path"] = a["path"]
root = os.path.realpath(scratch)
bad = [k for k, p in paths.items() if os.path.realpath(p) != root and not os.path.realpath(p).startswith(root + os.sep)]
for k, p in paths.items(): print(f"  {k}: {p}")
if bad:
    print("REFUSING: resolved path(s) outside the scratch:", bad); sys.exit(3)
PY

schema_of() { python3 -c "import sqlite3,sys; print(sqlite3.connect(sys.argv[1]).execute('SELECT version FROM schema_migrations_log').fetchone()[0])" "$1"; }
config_version_of() { python3 -c "import json,sys; print(json.load(open(sys.argv[1])).get('version',1))" "$1"; }
expect() { # $1 = what, $2 = got, $3 = want
  if [ "$2" != "$3" ]; then echo "  FAIL: $1 = $2, want $3"; exit 1; fi; echo "  ok: $1 = $2"
}
confine_check() { # re-read the (possibly daemon-rewritten) config: every path still under scratch
python3 - "$SCRATCH/config.json" "$SCRATCH" <<'PY'
import json, sys, os
doc = json.load(open(sys.argv[1])); root = os.path.realpath(sys.argv[2])
paths = {"data_dir": doc.get("data_dir",""), "datastore.path": doc.get("datastore",{}).get("path","")}
for a in doc.get("qso_archives", []):
    if a.get("path"): paths[f"qso_archives[{a.get('id')}].path"] = a["path"]
bad = [k for k, p in paths.items() if not p or (os.path.realpath(p) != root and not os.path.realpath(p).startswith(root + os.sep))]
if bad: print("  FAIL: path(s) outside the scratch after the daemon ran:", bad); sys.exit(1)
print("  ok: every configured database path is under the scratch")
PY
}

fingerprint() { # $1 = db path → per table: row count, then one sha256 PER COLUMN over every
                # row's (key, value), rows sorted by key, NULL and blobs encoded explicitly.
                # Per-column hashes let the verdict compare every column both schemas share
                # and name the ones a down migration drops, instead of hashing ids alone
                # (review 7d99af72: an id-only fingerprint would pass a rewritten qso.call).
python3 - "$1" <<'PY'
import sqlite3, sys, hashlib
db = sqlite3.connect(sys.argv[1])
tables = [r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table'")]
keyed = {"qso": "uuid", "qso_history": "id", "qso_upload": "id", "logbook": "id", "operator_event": "id"}
def enc(v):
    if v is None: return "NULL"
    if isinstance(v, bytes): return "blob:" + hashlib.sha256(v).hexdigest()
    return type(v).__name__ + ":" + repr(v)
for t, key in keyed.items():
    if t not in tables: print(f"{t}: absent"); continue
    cols = [r[1] for r in db.execute(f"PRAGMA table_info({t})")]
    rows = sorted(db.execute(f"SELECT {key}, {', '.join(cols)} FROM {t}").fetchall(), key=lambda r: str(r[0]))
    print(f"{t}: {len(rows)} rows")
    for i, c in enumerate(cols):
        h = hashlib.sha256("\n".join(f"{r[0]}\t{enc(r[i+1])}" for r in rows).encode()).hexdigest()[:16]
        print(f"{t}.{c} {h}")
v = db.execute("SELECT version, dirty FROM schema_migrations_log").fetchone() if "schema_migrations_log" in tables else None
print(f"schema: {v}")
PY
}

verdict() { # $1 = before fingerprint, $2 = after fingerprint, $3 = exact expected drop set →
            # 0 only if every row count and every shared column's value hash is identical AND the
            # columns absent after are EXACTLY the expected set (a down migration may drop what the
            # older schema never had, never anything else) AND nothing appears only after.
python3 - "$1" "$2" "$3" <<'PY'
import sys
expected = set(x.strip() for x in sys.argv[3].split(",") if x.strip())
def parse(text):
    counts, cols = {}, {}
    for ln in text.splitlines():
        if ln.startswith("schema:"): continue
        head, _, rest = ln.partition(" ")
        if head.endswith(":"): counts[head[:-1]] = rest
        else: cols[head] = rest
    return counts, cols
bc, bcols = parse(sys.argv[1]); ac, acols = parse(sys.argv[2])
bad = []
for t in bc:
    if bc[t] != ac.get(t): bad.append(f"{t}: {bc[t]} before, {ac.get(t)} after")
for c in sorted(set(bcols) & set(acols)):
    if bcols[c] != acols[c]: bad.append(f"{c}: value hash changed {bcols[c]} -> {acols[c]}")
dropped = set(bcols) - set(acols); added = sorted(set(acols) - set(bcols))
if added: bad.append(f"columns present only after: {added}")
if dropped - expected: bad.append(f"columns dropped that the drill did not expect: {sorted(dropped - expected)}")
if expected - dropped: bad.append(f"columns expected to be dropped were not (still present, or never in the source copy's schema): {sorted(expected - dropped)}")
if bad:
    print("  ROWS DIFFER:"); [print("    " + b) for b in bad]; sys.exit(1)
print("  ROWS IDENTICAL before -> after: every row count and every shared column's value hash")
print(f"  columns dropped by the downgrade, exactly as expected: {sorted(dropped) if dropped else 'none'}")
PY
}

start_daemon() { # $1 = binary, $2 = phase name; waits for /v1/version, prints it
  local bin="$1" phase="$2"
  rm -f "$SCRATCH/smd.sock"
  SM_WORKING_DIR="$SCRATCH" "$bin" --config "$SCRATCH/config.json" >"$SCRATCH/$phase.log" 2>&1 &
  PID=$!
  for _ in $(seq 1 120); do
    if out="$(curl -s --max-time 2 --unix-socket "$SCRATCH/smd.sock" http://localhost/v1/version 2>/dev/null)" && [ -n "$out" ]; then
      echo "$out" >"$SCRATCH/$phase.version"; echo "  $phase version: $out"; return 0
    fi
    if ! kill -0 "$PID" 2>/dev/null; then echo "  $phase: daemon exited early:"; tail -5 "$SCRATCH/$phase.log"; return 1; fi
    sleep 0.5
  done
  echo "  $phase: no answer within 60 s"; tail -5 "$SCRATCH/$phase.log"; return 1
}
stop_daemon() {
  kill -TERM "$PID"; for _ in $(seq 1 60); do kill -0 "$PID" 2>/dev/null || break; sleep 0.5; done
  wait "$PID" 2>/dev/null || true; PID=""
}

echo "drill[$LABEL]: fingerprint of the copy BEFORE any binary touches it"
BEFORE="$(fingerprint "$SCRATCH/db/station-manager.db")"; echo "$BEFORE" | sed 's/^/  /'

echo "drill[$LABEL]: phase 1 — NEW binary opens and migrates the copy up"
start_daemon "$NEW" "new"
stop_daemon
expect "new daemon reported schema" "$(grep -o '"schema":{"version":[0-9]*' "$SCRATCH/new.version" | grep -o '[0-9]*$')" "$NEW_SCHEMA"
expect "db schema after new start" "$(schema_of "$SCRATCH/db/station-manager.db")" "$NEW_SCHEMA"
expect "config version after new start" "$(config_version_of "$SCRATCH/config.json")" "$NEW_CONFIG"
confine_check
UP="$(fingerprint "$SCRATCH/db/station-manager.db")"; echo "$UP" | sed 's/^/  /'

echo "drill[$LABEL]: phase 2 — downgrade db → schema $OLD_SCHEMA, config → v$OLD_CONFIG (NEW binary)"
if [ "$NEW_SCHEMA" -gt "$OLD_SCHEMA" ]; then
  SM_WORKING_DIR="$SCRATCH" "$NEW" db-downgrade --to "$OLD_SCHEMA" --yes --config "$SCRATCH/config.json" | sed 's/^/  /'
else
  echo "  schema unchanged between builds ($NEW_SCHEMA): the db step is not under test in this run"
fi
if [ "$NEW_CONFIG" -gt "$OLD_CONFIG" ]; then
  SM_WORKING_DIR="$SCRATCH" "$NEW" config-downgrade --to "$OLD_CONFIG" --yes --config "$SCRATCH/config.json" | sed 's/^/  /'
else
  echo "  config version unchanged between builds ($NEW_CONFIG): the config step is not under test in this run"
fi
if [ "$MUTATE" = 1 ]; then
  # Proof hook, never for a real drill: change ONE retained value in the scratch
  # copy so the verdict must report ROWS DIFFER. A drill that stays green with
  # this flag has a fingerprint that is not looking at the data.
  python3 - "$SCRATCH/db/station-manager.db" <<'PY' || exit 1
import sqlite3, sys
with sqlite3.connect(sys.argv[1]) as db:
    row = db.execute("SELECT rowid, call FROM qso ORDER BY rowid LIMIT 1").fetchone()
    if row is None:
        raise SystemExit("  FAIL: --mutate-one-row needs a QSO in the scratch copy")
    replacement = "DR1LM" if row[1] == "DR1LL" else "DR1LL"
    changed = db.execute("UPDATE qso SET call = ? WHERE rowid = ?", (replacement, row[0]))
    if changed.rowcount != 1:
        raise SystemExit("  FAIL: --mutate-one-row did not update exactly one QSO")
PY
  echo "  MUTATED one qso.call in the scratch copy (proof run)"
fi
expect "db schema after downgrade" "$(schema_of "$SCRATCH/db/station-manager.db")" "$OLD_SCHEMA"
expect "config version after downgrade" "$(config_version_of "$SCRATCH/config.json")" "$OLD_CONFIG"
DOWN="$(fingerprint "$SCRATCH/db/station-manager.db")"; echo "$DOWN" | sed 's/^/  /'

echo "drill[$LABEL]: phase 3 — OLD binary opens the downgraded copy"
start_daemon "$OLD" "old"
stop_daemon
expect "old daemon reported schema" "$(grep -o '"schema":{"version":[0-9]*' "$SCRATCH/old.version" | grep -o '[0-9]*$')" "$OLD_SCHEMA"
expect "db schema after old start" "$(schema_of "$SCRATCH/db/station-manager.db")" "$OLD_SCHEMA"
confine_check
AFTER="$(fingerprint "$SCRATCH/db/station-manager.db")"; echo "$AFTER" | sed 's/^/  /'

echo "drill[$LABEL]: verdict"
verdict "$BEFORE" "$AFTER" "$EXPECT_DROPPED"
echo "$AFTER" | grep '^schema:' | sed 's/^/  /'
