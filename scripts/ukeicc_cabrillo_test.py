"""Tests for scripts/ukeicc-cabrillo.py:  python3 -I scripts/ukeicc_cabrillo_test.py"""
import importlib.util
import io
import os
import sys
import unittest
from contextlib import redirect_stderr, redirect_stdout

_here = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("cab", os.path.join(_here, "ukeicc-cabrillo.py"))
cab = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(cab)

ARGS = ["--contest", "UKEIDXSSB", "--callsign", "7Q5MLV", "--start", "2026-10-31T12:00",
        "--power", "LOW", "--assisted", "NON-ASSISTED", "--time", "24-HOURS"]


def tag(k, v):
    return f"<{k}:{len(v.encode())}>{v}"


def rec(call, stx, *, date="20261031", on="120100", off="120230", date_off=None, freq="14.200000",
        srx="36", district="", contest="UKEIDXSSB", mode="SSB", band=None, start="20261031", submode=""):
    band = band or {"28": "10m", "14": "20m", "7.": "40m", "3.": "80m", "21": "15m", "50": "6m"}[freq[:2]]
    f = {"CALL": call, "QSO_DATE": date, "TIME_ON": on, "TIME_OFF": off, "FREQ": freq, "MODE": mode,
         "BAND": band, "RST_SENT": "59", "RST_RCVD": "59", "CONTEST_ID": contest, "STX": stx,
         "SRX": srx, "SRX_STRING": district, "QSO_DATE_OFF": date_off or date,
         "APP_SM_CONTEST_START": start, "SUBMODE": submode}
    return "".join(tag(k, v) for k, v in f.items() if v != "") + "<EOR>\n"


TMP = os.environ.get("TMPDIR", "/tmp")
OUT = os.path.join(TMP, f"cab-test-{os.getpid()}.cbr")


def run(adif_text, keep_out=False):
    """(exit code, Cabrillo lines written to -o — [] when none, stderr)."""
    path = os.path.join(TMP, f"cab-test-{os.getpid()}.adi")
    with open(path, "w", encoding="utf-8") as fh:
        fh.write("Station Manager export<EOH>\n" + adif_text)
    if not keep_out and os.path.exists(OUT):
        os.remove(OUT)
    out, err = io.StringIO(), io.StringIO()
    try:
        with redirect_stdout(out), redirect_stderr(err):
            code = cab.main([path, "-o", OUT] + ARGS)
    finally:
        os.remove(path)
    lines = open(OUT, encoding="utf-8").read().splitlines() if os.path.exists(OUT) else []
    if not keep_out and os.path.exists(OUT):
        os.remove(OUT)
    return code, lines, err.getvalue()


def qso_lines(lines):
    return [line for line in lines if line.startswith("QSO:")]


class Converter(unittest.TestCase):
    def test_sample_shape_and_headers(self):
        code, lines, err = run(rec("G4ABC", "1", district="OX") + rec("ON4SS", "2", srx="19", freq="28.0244"))
        self.assertEqual(code, 0, err)
        self.assertEqual(lines[0], "START-OF-LOG: 3.0")
        for h in ["CONTEST: UKEIDXSSB", "CALLSIGN: 7Q5MLV", "CATEGORY-POWER: LOW",
                  "CATEGORY-ASSISTED: NON-ASSISTED", "CATEGORY-TIME: 24-HOURS"]:
            self.assertIn(h, lines)
        self.assertEqual(lines[-1], "END-OF-LOG:")
        q = [l.split() for l in qso_lines(lines)]
        # The sponsor's sample: QSO: 28024 PH 2014-07-26 1338 DL1AA 599 001 -- ON4SS 599 019 --
        self.assertEqual(q[1], ["QSO:", "28024", "PH", "2026-10-31", "1202", "7Q5MLV", "59", "002", "--",
                                "ON4SS", "59", "019", "--"])
        self.assertEqual(q[0][-1], "OX")

    def test_order_is_by_serial_sent_numerically_not_time_or_text(self):
        text = (rec("G4AAA", "10", off="130000", district="OX") + rec("G4BBB", "9", off="140000", district="OX")
                + rec("G4CCC", "100", off="120500", district="OX"))
        code, lines, err = run(text)
        self.assertEqual(code, 0, err)
        self.assertEqual([l.split()[7] for l in qso_lines(lines)], ["009", "010", "100"])

    def test_time_is_completion_truncated_to_the_minute(self):
        _, lines, _ = run(rec("G4ABC", "1", on="125830", off="125959", district="OX"))
        self.assertEqual(qso_lines(lines)[0].split()[4], "1259")

    def test_window_is_start_inclusive_end_exclusive_on_completion(self):
        text = (rec("G4IN", "1", on="115900", off="120000", district="OX")       # completes at 12:00 Sat: in
                + rec("G4OUT", "2", on="115800", off="115959", district="OX")    # completes before: out
                + rec("G4LAST", "3", date="20261101", on="115900", off="115959", district="OX")  # in
                + rec("G4END", "4", date="20261101", on="115930", off="120000", district="OX"))  # 12:00 Sun: out
        # A contact of THIS event completed outside its window cannot be
        # submitted: it stops the export (recorded ruling) rather than vanish.
        code, lines, err = run(text)
        self.assertEqual(code, 1)
        self.assertEqual(lines, [])
        self.assertIn("ERROR: G4OUT", err)
        self.assertIn("ERROR: G4END", err)
        code, lines, err = run(text.replace(rec("G4OUT", "2", on="115800", off="115959", district="OX"), "")
                               .replace(rec("G4END", "4", date="20261101", on="115930", off="120000",
                                            district="OX"), ""))
        self.assertEqual(code, 0, err)
        self.assertEqual([l.split()[9] for l in qso_lines(lines)], ["G4IN", "G4LAST"])

    def test_midnight_uses_the_end_date(self):
        _, lines, _ = run(rec("G4MID", "5", on="235930", off="000110", date_off="20261101", district="OX"))
        self.assertEqual(qso_lines(lines)[0].split()[3:5], ["2026-11-01", "0001"])

    def test_duplicate_serials_are_kept_and_listed(self):
        code, lines, err = run(rec("G4ABC", "1", district="OX") + rec("EI2XX", "1", district="DL"))
        self.assertEqual(code, 0, err)
        self.assertEqual(len(qso_lines(lines)), 2)
        self.assertIn("serial 001 sent more than once", err)

    def test_duplicate_contacts_on_a_band_are_kept_and_listed(self):
        code, lines, err = run(rec("G4ABC", "1", district="OX") + rec("G4ABC", "2", district="OX"))
        self.assertEqual(code, 0, err)
        self.assertEqual(len(qso_lines(lines)), 2)
        self.assertIn("G4ABC worked more than once on 20m", err)
        self.assertNotIn("sent more than once", err)
        # The same station on another band is not a duplicate.
        code, _, err = run(rec("G4ABC", "1", district="OX") + rec("G4ABC", "2", district="OX", freq="7.100000"))
        self.assertNotIn("more than once", err)

    def test_am_fm_and_cw_are_not_this_ssb_event(self):
        for mode, submode in [("AM", ""), ("FM", ""), ("CW", ""), ("SSB", "AM")]:
            code, lines, _ = run(rec("G4ABC", "1", district="OX", mode=mode, submode=submode))
            self.assertEqual(code, 1, mode)
            self.assertEqual(lines, [], mode)
        for submode in ["", "USB", "LSB"]:
            code, _, err = run(rec("G4ABC", "1", district="OX", submode=submode))
            self.assertEqual(code, 0, err)

    def test_only_this_occurrence_is_exported(self):
        text = (rec("G4DRY", "1", district="OX", start="20261030")    # a dry run under another date
                + rec("G4OLD", "7", district="OX", start="")          # no occurrence recorded
                + rec("G4REAL", "1", district="OX"))
        code, lines, err = run(text)
        self.assertEqual(code, 0, err)
        self.assertEqual([l.split()[9] for l in qso_lines(lines)], ["G4REAL"])
        self.assertIn("G4DRY", err)
        self.assertIn("G4OLD", err)

    def test_a_failed_conversion_leaves_an_earlier_export_untouched(self):
        with open(OUT, "w", encoding="utf-8") as fh:
            fh.write("EARLIER EXPORT\n")
        try:
            code, lines, _ = run(rec("G4ABC", "1"), keep_out=True)   # blank UK/EI district: error
            self.assertEqual(code, 1)
            self.assertEqual(lines, ["EARLIER EXPORT"])
            code, lines, err = run(rec("G4ABC", "1", district="OX"), keep_out=True)
            self.assertEqual(code, 0, err)
            self.assertEqual(lines[0], "START-OF-LOG: 3.0")
        finally:
            if os.path.exists(OUT):
                os.remove(OUT)

    def test_issues_are_written_to_a_file_beside_the_output(self):
        issues = OUT + ".issues.txt"
        try:
            # Errors: no Cabrillo, but the issues file lists them.
            code, lines, err = run(rec("G4ABC", "1", district="QQ"))
            self.assertEqual(code, 1)
            self.assertEqual(lines, [])
            text = open(issues, encoding="utf-8").read()
            self.assertIn("ERROR: G4ABC", text)
            self.assertIn("not a UK/EI District Code", text)
            # Notes only: the Cabrillo is written, and so are the notes.
            code, lines, err = run(rec("G4A", "1", district="OX") + rec("G4B", "3", district="OX"))
            self.assertEqual(code, 0, err)
            self.assertIn("NOTE: unused serials", open(issues, encoding="utf-8").read())
            # Clean: no issues file is left behind to mislead.
            code, lines, err = run(rec("G4A", "1", district="OX"))
            self.assertEqual(code, 0, err)
            self.assertFalse(os.path.exists(issues))
        finally:
            if os.path.exists(issues):
                os.remove(issues)

    def test_unreadable_input_is_written_to_the_issues_file(self):
        issues = OUT + ".issues.txt"
        try:
            code, lines, _ = run(rec("G4ABC", "1", district="OX").replace("<EOR>", ""))
            self.assertEqual(code, 1)
            self.assertIn("ERROR: ADIF", open(issues, encoding="utf-8").read())
        finally:
            if os.path.exists(issues):
                os.remove(issues)

    def test_outside_the_segment_says_all_qsos_are_disallowed(self):
        code, lines, err = run(rec("G4ABC", "1", district="OX", freq="14.100000"))
        self.assertEqual(code, 0, err)
        self.assertIn("all QSOs", err)

    def test_received_serial_typed_0_is_the_explicit_not_copied_record(self):
        code, lines, err = run(rec("G4ABC", "1", srx="0", district="OX"))
        self.assertEqual(code, 0, err)
        self.assertEqual(qso_lines(lines)[0].split()[11], "000")

    def test_blank_received_serial_stops_the_export(self):
        code, lines, err = run(rec("G4ABC", "1", srx="", district="OX"))
        self.assertEqual(code, 1)
        self.assertEqual(lines, [])
        self.assertIn("G4ABC", err)

    def test_district_typed_dashes_is_explicit_and_dx_needs_none(self):
        code, lines, err = run(rec("M0XYZ", "1", district="--") + rec("K1AA", "2"))
        self.assertEqual(code, 0, err)
        self.assertEqual([l.split()[-1] for l in qso_lines(lines)], ["--", "--"])

    def test_blank_district_for_a_ukei_call_stops_the_export(self):
        for call in ["M0XYZ", "G4ABC", "2E0AAA", "EI2XX", "GM3ABC", "MW0ABC"]:
            code, lines, err = run(rec(call, "1"))
            self.assertEqual(code, 1, call)
            self.assertEqual(lines, [], call)

    def test_wrong_mode_band_or_syntax_stops_the_export(self):
        bad = [
            rec("G4ABC", "1", district="OX", mode="CW"),                   # CW in the SSB contest
            rec("G4ABC", "1", district="OX", freq="50.150000"),            # 6 m
            rec("G4ABC", "1", district="O X"),                             # district with a space
            rec("G4ABC", "1", district="OX1"),
            rec("G4ABC", "1x", district="OX"),                             # serial sent not digits
            rec("G4ABC", "1", district="OX", srx="3a"),
        ]
        for text in bad:
            code, lines, _ = run(text)
            self.assertEqual(code, 1, text)
            self.assertEqual(lines, [], text)

    def test_a_district_not_in_appendix_2_stops_the_export(self):
        code, lines, err = run(rec("G4ABC", "1", district="QQ"))
        self.assertEqual(code, 1)
        self.assertEqual(lines, [])
        self.assertIn("not a UK/EI District Code", err)
        for ok in ["OX", "DU", "ZE", "EC", "--"]:
            code, _, err = run(rec("G4ABC", "1", district=ok))
            self.assertEqual(code, 0, (ok, err))

    def test_the_district_list_matches_the_phone_forms(self):
        ts = open(os.path.join(_here, "..", "frontend", "app", "src", "lib", "operate", "ukeiDistricts.ts"),
                  encoding="utf-8").read()
        import re
        form = set(re.findall(r"^\s+([A-Z]{2}):", ts, re.M))
        self.assertEqual(len(form), 155)
        self.assertEqual(set(cab.UKEI_DISTRICTS), form)

    def test_band_field_and_frequency_must_agree(self):
        code, lines, err = run(rec("G4ABC", "1", district="OX", freq="7.100000", band="20m"))
        self.assertEqual(code, 1)

    def test_truncated_adif_stops_the_export(self):
        whole = rec("G4ABC", "1", district="OX")
        partial = [whole + "<CALL:", whole + "<", whole + "<CALL:5", whole + "<EO"]  # cut inside a tag
        for text in [whole.replace("<EOR>", ""), whole[:whole.index("G4ABC") + 2]] + partial:
            code, lines, err = run(text)
            self.assertEqual(code, 1, text)
            self.assertEqual(lines, [])
            self.assertIn("ADIF", err)

    def test_non_members_are_ignored_and_a_member_without_stx_stops_the_export(self):
        code, lines, err = run(rec("G4NON", "", contest="") + rec("G4OK", "1", district="OX"))
        self.assertEqual(code, 0, err)
        self.assertEqual([l.split()[9] for l in qso_lines(lines)], ["G4OK"])
        self.assertNotIn("G4NON", err)
        code, lines, err = run(rec("G4NOS", "", district="OX") + rec("G4OK", "1", district="OX"))
        self.assertEqual(code, 1)
        self.assertIn("G4NOS", err)

    def test_gaps_are_noted_but_not_problems(self):
        code, _, err = run(rec("G4A", "1", district="OX") + rec("G4B", "3", district="OX"))
        self.assertEqual(code, 0)
        self.assertIn("gaps, expected): 002", err)

    def test_serials_beyond_999_are_not_truncated(self):
        code, lines, err = run(rec("G4A", "1000", srx="1234", district="OX"))
        self.assertEqual(code, 0, err)
        self.assertEqual(qso_lines(lines)[0].split()[7], "1000")
        self.assertEqual(qso_lines(lines)[0].split()[11], "1234")


import http.server
import json
import threading


def qso_json(call, stx, **kw):
    """One item as GET /v1/logbook/{id}/qso returns it (lower-case keys, MHz)."""
    q = {"call": call, "band": "20m", "mode": "SSB", "submode": "USB", "freq": "14.188",
         "qso_date": "20261031", "time_on": "120100", "time_off": "120230", "qso_date_off": "20261031",
         "rst_sent": "59", "rst_rcvd": "59", "contest_id": "UKEIDXSSB", "stx": stx, "srx": "36",
         "srx_string": "OX", "app_sm_contest_start": "20261031", "uuid": f"u-{call}-{stx}",
         "country_details": {"name": "x"}, "id": 7}
    q.update(kw)
    return q


class FakeDaemon:
    """GET /v1/version, /v1/logbook and the paged /v1/logbook/{id}/qso."""

    def __init__(self, pages, default_uuid="lb-uuid-1", logbooks=None):
        self.pages, self.requests = pages, []
        books = logbooks or [{"id": 1, "uuid": "lb-uuid-1"}, {"id": 2, "uuid": "lb-uuid-2"}]
        owner = self

        class H(http.server.BaseHTTPRequestHandler):
            def log_message(self, *a):
                pass

            def do_GET(self):
                owner.requests.append(self.path)
                if self.path == "/v1/version":
                    body = {"archive": {"default_logbook_uuid": default_uuid}}
                elif self.path == "/v1/logbook":
                    body = books
                elif self.path.startswith("/v1/logbook/1/qso"):
                    after = self.path.split("after=")[1].split("&")[0] if "after=" in self.path else ""
                    i = int(after or 0)
                    nxt = str(i + 1) if i + 1 < len(owner.pages) else None
                    body = {"items": owner.pages[i], "next_cursor": nxt}
                else:
                    self.send_response(404)
                    self.end_headers()
                    return
                data = json.dumps(body).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(data)

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()


def run_daemon(daemon, *extra):
    if os.path.exists(OUT):
        os.remove(OUT)
    out, err = io.StringIO(), io.StringIO()
    with redirect_stdout(out), redirect_stderr(err):
        code = cab.main(["--from-daemon", daemon.url, "-o", OUT] + list(extra) + ARGS)
    lines = open(OUT, encoding="utf-8").read().splitlines() if os.path.exists(OUT) else []
    if os.path.exists(OUT):
        os.remove(OUT)
    return code, lines, err.getvalue()


class FromDaemon(unittest.TestCase):
    """--from-daemon: every contact of the occurrence, whatever tab logged it."""

    def test_reads_every_page_of_the_default_logbook_and_keeps_only_this_event(self):
        d = FakeDaemon([
            [qso_json("G4AAA", "2"), qso_json("K1ABC", "", contest_id=None, app_sm_contest_start=None)],
            [qso_json("G4BBB", "1", srx="0"), qso_json("G4DRY", "1", app_sm_contest_start="20261030")],
        ])
        try:
            code, lines, err = run_daemon(d)
        finally:
            d.close()
        self.assertEqual(code, 0, err)
        q = [l.split() for l in qso_lines(lines)]
        self.assertEqual([x[9] for x in q], ["G4BBB", "G4AAA"])  # serial order, both pages
        self.assertEqual(q[0][1], "14188")
        self.assertEqual(q[0][11], "000")
        self.assertIn("G4DRY", err)  # another occurrence: listed, left out
        self.assertNotIn("K1ABC", err)  # not a contest contact: ignored
        self.assertTrue(any("/v1/logbook/1/qso" in r for r in d.requests))
        self.assertTrue(any("after=1" in r for r in d.requests))

    def test_a_named_logbook_is_read_instead_of_the_default(self):
        d = FakeDaemon([[qso_json("G4AAA", "1")]], default_uuid="lb-uuid-2")
        try:
            code, lines, err = run_daemon(d, "--logbook", "1")
        finally:
            d.close()
        self.assertEqual(code, 0, err)
        self.assertEqual(len(qso_lines(lines)), 1)

    def test_an_unreachable_daemon_writes_nothing(self):
        d = FakeDaemon([[qso_json("G4AAA", "1")]])
        url = d.url
        d.close()
        out, err = io.StringIO(), io.StringIO()
        if os.path.exists(OUT):
            os.remove(OUT)
        with redirect_stdout(out), redirect_stderr(err):
            code = cab.main(["--from-daemon", url, "-o", OUT] + ARGS)
        self.assertEqual(code, 1)
        self.assertFalse(os.path.exists(OUT))
        self.assertIn("ERROR", err.getvalue())

    def test_a_file_and_the_daemon_together_are_refused(self):
        with self.assertRaises(SystemExit):
            with redirect_stderr(io.StringIO()):
                cab.main(["x.adi", "--from-daemon", "http://127.0.0.1:1", "-o", OUT] + ARGS)


if __name__ == "__main__":
    unittest.main()
