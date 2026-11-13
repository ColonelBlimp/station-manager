"""Tests for scripts/smd_client.py:  python3 -I scripts/smd_client_test.py

W-0027 C2c: the client the operator's own tools use once smd's operating-client check is on.
Synthetic installs in temporary directories, synthetic identities made per run with openssl,
and listeners on free loopback ports. Never smd, never the station.
"""
import http.server
import importlib.util
import io
import json
import os
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import threading
import unittest
from contextlib import redirect_stderr, redirect_stdout

sys.dont_write_bytecode = True  # loading a sibling by path must not leave scripts/__pycache__
_here = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("smd_client", os.path.join(_here, "smd_client.py"))
sc = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(sc)

HEADER = "x-sm-operating-credential"
_made = []


def tmp(prefix):
    d = tempfile.mkdtemp(prefix=prefix)
    _made.append(d)
    return d


def tearDownModule():
    for d in _made:  # exactly the directories this run made
        os.chmod(d, 0o700)
        shutil.rmtree(d, ignore_errors=True)


def make_expired_identity(name):
    """A self-signed certificate for 127.0.0.1 whose validity ended in 2020. `openssl ca` sets the
    dates outright, which every OpenSSL version does the same way."""
    d = tmp(f"sm-client-id-{name}-")
    key, csr, cert = (os.path.join(d, n) for n in ("key.pem", "req.pem", "cert.pem"))
    conf = os.path.join(d, "ca.cnf")
    with open(conf, "w", encoding="utf-8") as f:
        f.write(f"""[ca]
default_ca = one
[one]
database = {d}/index
new_certs_dir = {d}
serial = {d}/serial
default_md = sha256
policy = any
unique_subject = no
[any]
commonName = supplied
[ext]
subjectAltName = IP:127.0.0.1
basicConstraints = critical,CA:FALSE
keyUsage = digitalSignature
extendedKeyUsage = serverAuth
""")
    open(os.path.join(d, "index"), "w").close()
    with open(os.path.join(d, "serial"), "w") as f:
        f.write("01\n")
    run = lambda *a: subprocess.run(["openssl", *a], check=True, capture_output=True)  # noqa: E731
    run("ecparam", "-name", "prime256v1", "-genkey", "-noout", "-out", key)
    run("req", "-new", "-key", key, "-out", csr, "-subj", f"/CN=Station Manager {name}")
    run("ca", "-selfsign", "-batch", "-notext", "-config", conf, "-extensions", "ext", "-keyfile", key,
        "-in", csr, "-out", cert, "-startdate", "20200101000000Z", "-enddate", "20200102000000Z")
    return key, cert


def make_identity(name, days="3650"):
    """A self-signed P-256 certificate for 127.0.0.1 shaped like smd's: not a CA."""
    d = tmp(f"sm-client-id-{name}-")
    key, cert = os.path.join(d, "key.pem"), os.path.join(d, "cert.pem")
    subprocess.run(
        ["openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes",
         "-keyout", key, "-out", cert, "-days", days, "-subj", f"/CN=Station Manager {name}",
         "-addext", "subjectAltName=IP:127.0.0.1", "-addext", "basicConstraints=critical,CA:FALSE",
         "-addext", "keyUsage=digitalSignature", "-addext", "extendedKeyUsage=serverAuth"],
        check=True, capture_output=True)
    return key, cert


GENUINE = make_identity("genuine")
IMPOSTOR = make_identity("impostor")
CRED = "c" * 42 + "A"  # 43 base64url characters, canonical, recognisable in an error


def install(port=None, *, cert=GENUINE[1], cred=CRED, data_dir=None, config=None):
    """A synthetic install laid out and permissioned as smd makes it. Returns its paths."""
    base = tmp("sm-client-")
    wd = os.path.join(base, "sm")
    os.mkdir(wd, 0o700)
    root = data_dir or wd
    os.makedirs(root, mode=0o700, exist_ok=True)
    cfg = dict(config or {})
    if port is not None:
        cfg.setdefault("server", {})["app_port"] = port
    if data_dir:
        cfg["data_dir"] = data_dir
    cfg_path = os.path.join(wd, "config.json")
    with open(os.open(cfg_path, os.O_WRONLY | os.O_CREAT, 0o600), "w") as f:
        json.dump(cfg, f)
    iddir, creddir = os.path.join(root, "desktop-identity"), os.path.join(root, "desktop-credential")
    os.mkdir(iddir, 0o700)
    shutil.copy(cert, os.path.join(iddir, "cert.pem"))
    os.chmod(os.path.join(iddir, "cert.pem"), 0o644)
    os.mkdir(creddir, 0o700)
    credfile = os.path.join(creddir, "credential")
    with open(os.open(credfile, os.O_WRONLY | os.O_CREAT, 0o600), "w") as f:
        f.write(cred)
    return {"wd": wd, "config": cfg_path, "root": root, "iddir": iddir, "cert": os.path.join(iddir, "cert.pem"),
            "creddir": creddir, "cred": credfile, "env": {"SM_WORKING_DIR": wd, "HOME": "/nonexistent"}}


class Listener:
    """A loopback listener that records every request it is sent. With `identity` it speaks TLS;
    without, plain HTTP. `answer(method, path)` gives (status, headers, body)."""

    def __init__(self, identity=None, *, answer=None, max_tls=None):
        owner = self
        self.requests, self.raw = [], []
        self.answer = answer or (lambda m, p: (200, {"Content-Type": "application/json"}, b'{"daemon":"fixture"}'))

        class H(http.server.BaseHTTPRequestHandler):
            def log_message(self, *a):
                pass

            def _serve(self):
                n = int(self.headers.get("Content-Length") or 0)
                body = self.rfile.read(n) if n else b""
                creds = [v for k, v in self.headers.items() if k.lower() == HEADER]
                owner.requests.append({"method": self.command, "path": self.path, "cred": creds,
                                       "host": self.headers.get("Host"), "body": body,
                                       "type": self.headers.get("Content-Type")})
                status, headers, out = owner.answer(self.command, self.path)
                self.send_response(status)
                for k, v in headers.items():
                    self.send_header(k, v)
                self.send_header("Content-Length", str(len(out)))
                self.end_headers()
                self.wfile.write(out)

            do_GET = do_POST = do_PUT = do_DELETE = _serve

            def parse_request(self):
                owner.raw.append(self.raw_requestline)  # whatever arrived, TLS bytes included
                return super().parse_request()

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
        self.server.handle_error = lambda *a: None  # a refused client is the point, not a fault
        if identity:
            ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
            ctx.load_cert_chain(identity[1], identity[0])
            ctx.minimum_version = ssl.TLSVersion.TLSv1_2
            if max_tls:
                ctx.maximum_version = max_tls
            self.server.socket = ctx.wrap_socket(self.server.socket, server_side=True)
        self.port = self.server.server_address[1]
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def close(self):
        self.server.shutdown()
        self.server.server_close()

    def saw_credential(self, value=CRED):
        blob = b"".join(self.raw) + b"".join(r["body"] for r in self.requests)
        return value.encode() in blob or any(r["cred"] for r in self.requests)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class Discovery(unittest.TestCase):
    def test_the_default_layout_and_port(self):
        inst = install()
        got = sc.discover(env=inst["env"])
        self.assertEqual(got.port, 8443)
        self.assertEqual(got.cert_path, inst["cert"])
        self.assertEqual(got.credential_path, inst["cred"])

    def test_an_explicit_port_a_data_dir_elsewhere_and_an_explicit_config(self):
        elsewhere = os.path.join(tmp("sm-client-data-"), "data")
        inst = install(9443, data_dir=elsewhere)
        got = sc.discover(config=inst["config"], env={"HOME": "/nonexistent"})
        self.assertEqual(got.port, 9443)
        self.assertEqual(got.cert_path, os.path.join(elsewhere, "desktop-identity", "cert.pem"))
        self.assertEqual(got.credential_path, os.path.join(elsewhere, "desktop-credential", "credential"))

    def test_the_xdg_default_location(self):
        inst = install()
        xdg = tmp("sm-client-xdg-")
        os.rename(inst["wd"], os.path.join(xdg, "station-manager"))
        got = sc.discover(env={"XDG_DATA_HOME": xdg, "HOME": "/nonexistent"})
        self.assertEqual(got.cert_path, os.path.join(xdg, "station-manager", "desktop-identity", "cert.pem"))

    def test_the_credential_is_never_printed(self):
        got = sc.discover(env=install()["env"])
        for text in (repr(got), str(got), f"{got}", repr(sc.Client(got)), str(vars(got))):
            self.assertNotIn(CRED, text)

    def refused(self, spoil, names, *, cfg=None, uid=None, cred=CRED):
        inst = install(config=cfg, cred=cred)
        target = spoil(inst) if spoil else None
        with self.assertRaises(sc.ClientError) as cm:
            sc.discover(env=inst["env"], **({"uid": uid} if uid is not None else {}))
        msg = str(cm.exception)
        self.assertIn(names if isinstance(names, str) else names(inst), msg)
        self.assertNotIn(CRED, msg)
        if cred != CRED and len(cred.strip()) >= 8:
            self.assertNotIn(cred.strip(), msg)
        return msg, target

    def test_unsafe_modes_and_shapes_are_refused_by_path(self):
        def symlink(path):
            def do(i):
                real = os.path.join(i["root"], "elsewhere")
                os.rename(i[path], real)
                os.symlink(real, i[path])
            return do
        cases = [
            ("identity directory 0755", lambda i: os.chmod(i["iddir"], 0o755), "desktop-identity"),
            ("certificate group-writable", lambda i: os.chmod(i["cert"], 0o664), "cert.pem"),
            ("certificate a symlink", symlink("cert"), "cert.pem"),
            ("certificate missing", lambda i: os.remove(i["cert"]), "cert.pem"),
            ("certificate malformed", lambda i: open(i["cert"], "w").write("not a certificate"), "cert.pem"),
            ("credential directory 0755", lambda i: os.chmod(i["creddir"], 0o755), "desktop-credential"),
            ("credential directory a symlink", symlink("creddir"), "desktop-credential"),
            ("credential 0640", lambda i: os.chmod(i["cred"], 0o640), "credential"),
            ("credential 0604", lambda i: os.chmod(i["cred"], 0o604), "credential"),
            ("credential a symlink", symlink("cred"), "credential"),
            ("credential missing", lambda i: os.remove(i["cred"]), "credential"),
            ("no credential directory", lambda i: shutil.rmtree(i["creddir"]), "desktop-credential"),
            ("config.json group-writable", lambda i: os.chmod(i["config"], 0o620), "config.json"),
            ("the working directory writable by others", lambda i: os.chmod(i["wd"], 0o777), lambda i: i["wd"]),
        ]
        for name, spoil, names in cases:
            with self.subTest(name):
                self.refused(spoil, names)

    def test_a_malformed_credential_is_refused_and_never_quoted(self):
        good = "A" * 43
        for name, text in [("empty", ""), ("short", good[1:]), ("long", good + "A"), ("newline", good + "\n"),
                           ("a space", " " + good[1:]), ("plus", "+" + good[1:]), ("slash", "/" + good[1:]),
                           ("padding", good[1:] + "="), ("non-canonical final bits", good[1:] + "B"),
                           ("not ASCII", "é" * 22)]:
            with self.subTest(name):
                msg, _ = self.refused(None, "credential", cred=text)
                self.assertIn("43", msg)

    def test_bad_config_values_are_refused(self):
        for name, cfg, names in [
            ("a port out of range", {"server": {"app_port": 70000}}, "app_port"),
            ("a port that is not a number", {"server": {"app_port": "8443"}}, "app_port"),
            ("a relative data_dir", {"data_dir": "data"}, "data_dir"),
        ]:
            with self.subTest(name):
                self.refused(None, names, cfg=cfg)

    def test_files_of_another_user_are_refused(self):
        self.refused(None, "owned by", uid=os.geteuid() + 4242)

    def test_no_install_is_an_error_that_names_the_config(self):
        d = tmp("sm-client-none-")
        with self.assertRaises(sc.ClientError) as cm:
            sc.discover(env={"SM_WORKING_DIR": d, "HOME": "/nonexistent"})
        self.assertIn(os.path.join(d, "config.json"), str(cm.exception))

    def test_a_relative_location_is_refused(self):
        for kw in ({"config": "config.json", "env": {}}, {"env": {"SM_WORKING_DIR": "sm"}}, {"env": {}}):
            with self.assertRaises(sc.ClientError):
                sc.discover(**kw)


class Requests(unittest.TestCase):
    def client(self, listener, **kw):
        inst = install(listener.port, **kw)
        return sc.Client(sc.discover(env=inst["env"]))

    def test_the_genuine_daemon_is_asked_with_exactly_one_credential(self):
        d = Listener(GENUINE)
        try:
            c = self.client(d)
            status, body = c.request("GET", "/v1/version")
            self.assertEqual((status, json.loads(body)), (200, {"daemon": "fixture"}))
            status, _ = c.request("POST", "/v1/fixture/thing", json_body={"active": True})
            self.assertEqual(status, 200)
        finally:
            d.close()
        self.assertEqual([r["cred"] for r in d.requests], [[CRED], [CRED]])
        self.assertEqual(d.requests[0]["host"], f"127.0.0.1:{d.port}")
        self.assertEqual((d.requests[1]["method"], d.requests[1]["path"]), ("POST", "/v1/fixture/thing"))
        self.assertEqual(json.loads(d.requests[1]["body"]), {"active": True})
        self.assertEqual(d.requests[1]["type"], "application/json")

    def refused_before_anything_is_sent(self, listener, **kw):
        try:
            c = self.client(listener, **kw)
            with self.assertRaises(sc.ClientError) as cm:
                c.request("GET", "/v1/version")
        finally:
            listener.close()
        self.assertNotIn(CRED, str(cm.exception))
        self.assertEqual(listener.requests, [], "a request reached a listener that is not smd")
        self.assertFalse(listener.saw_credential(), "the credential reached a listener that is not smd")
        return str(cm.exception)

    def test_an_impostor_with_another_key_is_refused_before_anything_is_sent(self):
        msg = self.refused_before_anything_is_sent(Listener(IMPOSTOR))
        self.assertIn("identity", msg)

    def test_the_pin_is_the_rule_as_in_the_desktop_app_dates_are_not_enforced(self):
        # smd's identity is pinned, not validated against a clock (ADR 0103; operator ruling
        # 2026-11-10). A tool must not refuse an install the desktop app still opens.
        expired = make_expired_identity("expired")
        end = subprocess.run(["openssl", "x509", "-in", expired[1], "-noout", "-checkend", "0"], capture_output=True)
        self.assertNotEqual(end.returncode, 0, "the fixture certificate has not expired: the test proves nothing")
        d = Listener(expired)
        try:
            status, _ = self.client(d, cert=expired[1]).request("GET", "/v1/version")
        finally:
            d.close()
        self.assertEqual(status, 200)
        self.assertEqual(d.requests[0]["cred"], [CRED])

    def test_a_plain_http_listener_at_the_port_never_sees_the_credential(self):
        self.refused_before_anything_is_sent(Listener(None))

    def test_tls_older_than_1_3_is_refused(self):
        self.refused_before_anything_is_sent(Listener(GENUINE, max_tls=ssl.TLSVersion.TLSv1_2))

    def test_nothing_listening_is_an_error_without_the_credential(self):
        inst = install(free_port())
        with self.assertRaises(sc.ClientError) as cm:
            sc.Client(sc.discover(env=inst["env"])).request("GET", "/v1/version")
        self.assertNotIn(CRED, str(cm.exception))

    def test_a_redirect_is_returned_and_never_followed(self):
        other = Listener(GENUINE)
        plain = Listener(None)
        targets = iter([f"https://127.0.0.1:{other.port}/v1/version", f"http://127.0.0.1:{plain.port}/v1/version"])
        d = Listener(GENUINE, answer=lambda m, p: (302, {"Location": next(targets)}, b""))
        try:
            c = self.client(d)
            self.assertEqual(c.request("GET", "/v1/version")[0], 302)
            self.assertEqual(c.request("GET", "/v1/version")[0], 302)
        finally:
            for x in (d, other, plain):
                x.close()
        self.assertEqual(len(d.requests), 2)
        for x in (other, plain):
            self.assertEqual(x.requests, [], "a redirect was followed")
            self.assertFalse(x.saw_credential(), "the credential followed a redirect")

    def test_a_path_cannot_name_another_host_or_carry_a_header(self):
        d = Listener(GENUINE)
        try:
            c = self.client(d)
            for bad in ("http://evil.example/v1/version", "//evil.example/x", "v1/version", "",
                        "/v1/version\r\nX-Injected: 1", "/v1/ver sion"):
                with self.subTest(bad), self.assertRaises(sc.ClientError):
                    c.request("GET", bad)
        finally:
            d.close()
        self.assertEqual(d.requests, [])

    def test_a_refusal_by_smd_is_an_answer_not_an_error(self):
        body = json.dumps({"code": "desktop_app_required", "message": "x", "op": "y"}).encode()
        d = Listener(GENUINE, answer=lambda m, p: (403, {"Content-Type": "application/json"}, body))
        try:
            status, got = self.client(d).request("GET", "/v1/version")
        finally:
            d.close()
        self.assertEqual((status, got), (403, body))


class CommandLine(unittest.TestCase):
    """What the tune probe uses: the status code on stdout, like curl's %{http_code}. These
    tests have a network, so they name no rig route; the probe's own tests have none."""

    def run_cli(self, env, *argv):
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = sc.main(list(argv), env=env)
        return code, out.getvalue().strip(), err.getvalue()

    def test_prints_the_status_and_sends_the_body(self):
        d = Listener(GENUINE, answer=lambda m, p: (202, {}, b""))
        try:
            inst = install(d.port)
            code, out, err = self.run_cli(inst["env"], "POST", "/v1/fixture/thing", "--json", '{"active":false}')
        finally:
            d.close()
        self.assertEqual((code, out), (0, "202"), err)
        self.assertEqual(json.loads(d.requests[0]["body"]), {"active": False})
        self.assertEqual(d.requests[0]["cred"], [CRED])

    def test_an_unreachable_or_untrusted_daemon_prints_000_and_says_why_without_the_credential(self):
        d = Listener(IMPOSTOR)
        try:
            inst = install(d.port)
            code, out, err = self.run_cli(inst["env"], "GET", "/v1/version")
        finally:
            d.close()
        self.assertEqual((code, out), (1, "000"))
        self.assertIn("identity", err)
        self.assertNotIn(CRED, err + out)
        self.assertEqual(d.requests, [])

    def test_invalid_json_is_refused_before_anything_is_sent(self):
        d = Listener(GENUINE)
        try:
            inst = install(d.port)
            code, out, _ = self.run_cli(inst["env"], "POST", "/v1/fixture/thing", "--json", "{not json")
        finally:
            d.close()
        self.assertEqual((code, out), (1, "000"))
        self.assertEqual(d.requests, [])

    def test_the_command_line_takes_no_credential(self):
        with redirect_stdout(io.StringIO()) as out, self.assertRaises(SystemExit):
            sc.main(["--help"], env={})
        self.assertIn("--sm-config", out.getvalue())
        self.assertNotIn("cred", out.getvalue().lower(), "an option could put the credential in the process list")


if __name__ == "__main__":
    unittest.main()
