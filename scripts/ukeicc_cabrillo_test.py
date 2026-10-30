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


if __name__ == "__main__":
    unittest.main()
