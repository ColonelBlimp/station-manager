#!/usr/bin/env python3
"""A client for the local smd that the operator's own tools use (W-0027 C2c).

With smd's operating-client check on (server.require_desktop_app), smd serves only its pinned
HTTPS listener, and only a request that carries this install's operating credential. This
module is how a script of the operator's own user does that, the same way the desktop app does:

  - it finds the install as smd does: --sm-config, else SM_WORKING_DIR, else the XDG data
    directory; reads data_dir and server.app_port from config.json and nothing else;
  - it reads smd's certificate (desktop-identity/cert.pem) and the operating credential
    (desktop-credential/credential), refusing either if it is not the user's own, private file;
  - it verifies smd BEFORE it sends anything, by the desktop app's own rule (ADR 0103 point
    3): TLS 1.3 or later to 127.0.0.1, and the certificate the listener presents must be smd's
    certificate, byte for byte. Completing a TLS 1.3 handshake proves the listener holds that
    certificate's key (RFC 8446 section 4.4.3). Like the app's pin, the certificate's dates are
    not enforced (operator ruling 2026-11-10). Only then is the request, with the credential,
    written;
  - it never follows a redirect, never falls back to plain HTTP, and no error it raises and
    nothing it prints contains the credential. The credential is never a command-line argument.

It works whether the check is on or off: with it off, smd ignores the credential.

As a command it sends one request and prints the HTTP status, as curl's %{http_code} does
("000" when no answer was had), for shell scripts:

  python3 -I scripts/smd_client.py GET /v1/version
  python3 -I scripts/smd_client.py POST /v1/rig/tune --json '{"active":false}'

Standard library only. Linux.
"""
import argparse
import base64
import http.client
import json
import os
import ssl
import stat
import sys

HEADER = "X-SM-Operating-Credential"
HOST = "127.0.0.1"
DEFAULT_APP_PORT = 8443
CREDENTIAL_CHARS = 43  # 32 bytes in unpadded base64url: the whole file (internal/desktopid)


class ClientError(Exception):
    """Why the daemon could not be asked. The message names paths and ports, never the credential."""


class Install:
    """A validated install: where smd's pinned listener is and what to present to it. The
    credential is held where printing the object, or its attributes, does not show it."""

    def __init__(self, port, cert_path, cert_pem, credential_path, credential):
        self.port, self.cert_path, self.cert_pem = port, cert_path, cert_pem
        self.credential_path = credential_path
        self.reveal = lambda: credential

    def __repr__(self):
        return f"Install(port={self.port}, cert_path={self.cert_path!r}, credential_path={self.credential_path!r})"


def _config_path(config, env):
    def absolute(p, what):
        if not os.path.isabs(p):
            raise ClientError(f"{what} is not an absolute path; give --sm-config <absolute path>")
        return p
    if config:
        return absolute(config, "--sm-config")
    if env.get("SM_WORKING_DIR"):
        return os.path.join(absolute(env["SM_WORKING_DIR"], "SM_WORKING_DIR"), "config.json")
    if env.get("XDG_DATA_HOME"):
        base = absolute(env["XDG_DATA_HOME"], "XDG_DATA_HOME")
    elif env.get("HOME"):
        base = os.path.join(absolute(env["HOME"], "HOME"), ".local", "share")
    else:
        raise ClientError("HOME is not set; give --sm-config <absolute path>")
    return os.path.join(base, "station-manager", "config.json")


def _mode(st):
    return f"{stat.S_IMODE(st.st_mode):04o}"


def _read_private(path, uid, what, check):
    """Open path without following a symlink, check the open file, then read it."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
    except FileNotFoundError:
        raise ClientError(f"{what} {path} is missing") from None
    except OSError as e:
        reason = "is a symlink" if e.errno == 40 else f"cannot be opened ({e.strerror})"  # ELOOP
        raise ClientError(f"{what} {path} {reason}") from None
    try:
        st = os.fstat(fd)
        if not stat.S_ISREG(st.st_mode):
            raise ClientError(f"{what} {path} is not a regular file")
        if st.st_uid != uid:
            raise ClientError(f"{what} {path} is owned by uid {st.st_uid}, not {uid}")
        check(st)
        with os.fdopen(os.dup(fd), "rb") as f:
            return f.read(1 << 20)
    finally:
        os.close(fd)


def _private_dir(path, uid, what):
    try:
        st = os.lstat(path)
    except FileNotFoundError:
        raise ClientError(f"{what} {path} does not exist: smd makes it when it starts") from None
    if stat.S_ISLNK(st.st_mode):
        raise ClientError(f"{what} {path} is a symlink")
    if not stat.S_ISDIR(st.st_mode):
        raise ClientError(f"{what} {path} is not a directory")
    if st.st_uid != uid:
        raise ClientError(f"{what} {path} is owned by uid {st.st_uid}, not {uid}")
    if stat.S_IMODE(st.st_mode) != 0o700:
        raise ClientError(f"{what} {path} must be mode 0700 (is {_mode(st)})")


def _ancestors(resolved, uid):
    """Every directory from the resolved root up to /: the user's or root's, and not writable
    by group or others unless sticky (as smd and the desktop app check)."""
    p = resolved
    while True:
        st = os.lstat(p)
        if st.st_uid not in (uid, 0):
            raise ClientError(f"{p} is owned by uid {st.st_uid}, which could replace what is below it")
        if st.st_mode & 0o022 and not st.st_mode & stat.S_ISVTX:
            raise ClientError(f"{p} is writable by another user (mode {_mode(st)}), who could replace what is below it")
        parent = os.path.dirname(p)
        if parent == p:
            return
        p = parent


def discover(config=None, env=None, uid=None):
    """Find and validate the install. Raises ClientError, naming the path that is wrong."""
    env = os.environ if env is None else env
    uid = os.geteuid() if uid is None else uid
    cfg_path = _config_path(config, env)

    def cfg_ok(st):
        if st.st_mode & 0o022:
            raise ClientError(f"config.json {cfg_path} is writable by group or others (mode {_mode(st)})")
    try:
        doc = json.loads(_read_private_following(cfg_path, uid, cfg_ok))
    except ValueError:
        raise ClientError(f"config.json {cfg_path} is not valid JSON") from None  # never its text
    if not isinstance(doc, dict):
        raise ClientError(f"config.json {cfg_path} is not a JSON object")

    server = doc.get("server", {})
    port = server.get("app_port", 0) if isinstance(server, dict) else None
    if port in (0, None) and isinstance(server, dict):
        port = DEFAULT_APP_PORT
    if type(port) is not int or not 1 <= port <= 65535:
        raise ClientError(f"server.app_port in {cfg_path} must be an integer from 1 to 65535")

    data_dir = doc.get("data_dir", "")
    if data_dir == "":
        data_dir = os.path.dirname(cfg_path)
    elif not isinstance(data_dir, str) or not os.path.isabs(data_dir):
        raise ClientError(f"data_dir in {cfg_path} must be an absolute path")
    try:
        root = os.path.realpath(data_dir, strict=True)
    except OSError:
        raise ClientError(f"the data directory {data_dir} does not exist") from None
    _ancestors(root, uid)

    iddir = os.path.join(root, "desktop-identity")
    _private_dir(iddir, uid, "the identity directory")
    cert_path = os.path.join(iddir, "cert.pem")

    def cert_ok(st):
        if st.st_mode & 0o022:
            raise ClientError(f"the certificate {cert_path} is writable by group or others (mode {_mode(st)})")
    cert_pem = _read_private(cert_path, uid, "the certificate", cert_ok).decode("ascii", "replace")
    try:
        ssl.PEM_cert_to_DER_cert(cert_pem)
    except ValueError:
        raise ClientError(f"the certificate {cert_path} is not a PEM certificate") from None

    creddir = os.path.join(root, "desktop-credential")
    _private_dir(creddir, uid, "the credential directory")
    cred_path = os.path.join(creddir, "credential")

    def cred_ok(st):
        if stat.S_IMODE(st.st_mode) != 0o600:
            raise ClientError(f"the credential {cred_path} must be mode 0600 (is {_mode(st)})")
    raw = _read_private(cred_path, uid, "the credential", cred_ok)
    text = raw.decode("latin-1")
    canonical = len(text) == CREDENTIAL_CHARS
    if canonical:
        try:  # one secret has one text: decoding then re-encoding must give it back
            canonical = base64.urlsafe_b64encode(base64.urlsafe_b64decode(text + "=")).decode().rstrip("=") == text
        except ValueError:
            canonical = False
    if not canonical:
        raise ClientError(f"the credential {cred_path} is not valid: it must hold exactly "
                          f"{CREDENTIAL_CHARS} base64url characters")
    return Install(port, cert_path, cert_pem, cred_path, text)


def _read_private_following(path, uid, check):
    """config.json may be reached through a symlink (as smd reads it); it is still checked on
    the open file."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK)
    except FileNotFoundError:
        raise ClientError(f"no config.json at {path}: smd has not run here") from None
    except OSError as e:
        raise ClientError(f"config.json {path} cannot be opened ({e.strerror})") from None
    try:
        st = os.fstat(fd)
        if not stat.S_ISREG(st.st_mode):
            raise ClientError(f"config.json {path} is not a regular file")
        if st.st_uid != uid:
            raise ClientError(f"config.json {path} is owned by uid {st.st_uid}, not {uid}")
        check(st)
        with os.fdopen(os.dup(fd), "rb") as f:
            return f.read(8 << 20)
    finally:
        os.close(fd)


class Client:
    """Requests to the local smd over its pinned listener, each on its own connection."""

    def __init__(self, install, timeout=30):
        self._install, self._timeout = install, timeout
        # The pin is the whole rule, as in the desktop app: no certificate store is consulted
        # (the system's or any other), and the one comparison in request() decides.
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        ctx.minimum_version = ssl.TLSVersion.TLSv1_3
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        self._ctx = ctx
        self._pinned = ssl.PEM_cert_to_DER_cert(install.cert_pem)

    def __repr__(self):
        return f"Client({self._install!r})"

    def request(self, method, path, json_body=None):
        """Send one request. Returns (status, body bytes). A refusal by smd is an answer; only
        failing to reach and verify smd raises ClientError."""
        if not path.startswith("/") or path.startswith("//") or any(c <= " " or c == "\x7f" for c in path):
            raise ClientError("the request path must be an absolute path on the daemon, such as /v1/version")
        where = f"https://{HOST}:{self._install.port}"
        conn = http.client.HTTPSConnection(HOST, self._install.port, context=self._ctx, timeout=self._timeout)
        try:
            try:
                conn.connect()  # the handshake: nothing of ours has been sent yet
            except ssl.SSLError as e:
                raise ClientError(f"{where} did not prove smd's identity ({self._install.cert_path}): "
                                  f"{e.reason or 'TLS failure'}") from None
            except OSError as e:
                raise ClientError(f"smd does not answer at {where} ({e.strerror or type(e).__name__})") from None
            if conn.sock.getpeercert(binary_form=True) != self._pinned:
                raise ClientError(f"{where} presented a certificate that is not smd's identity "
                                  f"({self._install.cert_path})")
            headers = {HEADER: self._install.reveal()}
            body = None
            if json_body is not None:
                body = json.dumps(json_body).encode()
                headers["Content-Type"] = "application/json"
            try:
                conn.request(method, path, body=body, headers=headers)
                resp = conn.getresponse()  # http.client never follows a redirect
                return resp.status, resp.read()
            except (OSError, http.client.HTTPException) as e:
                raise ClientError(f"the request to {where} failed ({type(e).__name__})") from None
        finally:
            conn.close()


def local_client(config=None, env=None, timeout=30):
    """A Client for the local install."""
    return Client(discover(config, env), timeout=timeout)


def parser():
    """The command line. scripts/tune_probe_test.py checks what the tune probe passes against it."""
    p = argparse.ArgumentParser(description="Send one request to the local smd and print the HTTP status "
                                            "(000 when no answer was had).")
    p.add_argument("method", choices=["GET", "POST", "PUT", "PATCH", "DELETE"])
    p.add_argument("path", help="an absolute path on the daemon, e.g. /v1/version")
    p.add_argument("--json", metavar="TEXT", help="a JSON request body")
    p.add_argument("--sm-config", metavar="PATH", help="config.json of another install (an absolute path)")
    p.add_argument("--timeout", type=float, default=5.0)
    return p


def main(argv=None, env=None):
    a = parser().parse_args(argv)
    try:
        body = None
        if a.json is not None:
            try:
                body = json.loads(a.json)
            except ValueError:
                raise ClientError("--json is not valid JSON") from None
        status, _ = local_client(a.sm_config, env, timeout=a.timeout).request(a.method, a.path, json_body=body)
    except ClientError as e:
        print("000")
        print(f"smd_client: {e}", file=sys.stderr)
        return 1
    print(status)
    return 0


if __name__ == "__main__":
    sys.exit(main())
