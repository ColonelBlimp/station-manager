"""No TCP connections for tests that execute something able to command a rig (W-0027 C2c).

A test that runs scripts/tune-duration-probe.sh must not be able to reach a real daemon,
whatever the script, the client, or a deliberately broken copy of either does. Checking the
script's text cannot promise that (codex-114), and neither can allowing "the fixture's ports":
a Landlock network rule names a port number, not an address or a socket, so an allowed port is
allowed at every address and for whoever listens on it later (codex-115). So nothing is
allowed. Before anything is executed, the test process confines itself with Landlock with an
empty allow-list: it, its threads and every process it starts can open no TCP connection at
all, to any address or port. The kernel refuses each attempt (EACCES). A Landlock domain cannot
be left or widened, and children inherit it. The probe is therefore tested against a recording
stand-in for the client command, never against a listener.

    if __name__ == "__main__":
        confined.main()          # confines this process, then runs the file's tests

    confined.require()           # before executing the script: refuses if not confined

The boundary is measured before it is relied on, with synthetic endpoints only and never a
station address: before confining itself the process opens listeners of its own on two
loopback addresses. enter() then requires that connecting to each is refused with a permission
error although it is listening, from this process and from a child; otherwise nothing is run.
At exit the run fails if anything ever connected to one of them.

Limits. TCP only: a daemon's Unix socket is a file, not a port, and is not covered; the tests
give every subprocess a HOME and working directory of their own instead. Linux 6.7 or later
with Landlock enabled; where that is missing the run fails with the reason. The tests are
never run unconfined and never skipped.
"""
import atexit
import ctypes
import os
import socket
import struct
import subprocess
import sys
import threading
import unittest

_SYS_CREATE_RULESET, _SYS_RESTRICT_SELF = 444, 446
_CONNECT_TCP = 1 << 1  # LANDLOCK_ACCESS_NET_CONNECT_TCP
_PR_SET_NO_NEW_PRIVS = 38
_NET_ABI = 4  # the first Landlock ABI with network rules (Linux 6.7)
_OUTSIDE_ADDRESSES = ("127.0.0.1", "127.0.0.2")  # synthetic: this process's own listeners

_state = {"confined": False, "outside": [], "hits": 0}

_CHILD = ("import socket, sys\n"
          "for a in sys.argv[1:]:\n"
          "    host, port = a.rsplit(':', 1)\n"
          "    try:\n"
          "        socket.create_connection((host, int(port)), timeout=2).close(); print('reached')\n"
          "    except PermissionError:\n"
          "        print('denied')\n"
          "    except OSError as e:\n"
          "        print('error', e)\n")


class ConfinementError(Exception):
    pass


def _listen(host):
    s = socket.socket()
    s.bind((host, 0))
    s.listen(16)
    s.setblocking(False)
    return s


def attempt(address):
    """Try to open a TCP connection: 'denied' (the kernel refused it), 'reached', or 'error …'."""
    try:
        socket.create_connection(address, timeout=2).close()
    except PermissionError:
        return "denied"
    except OSError as e:
        return f"error {e}"
    return "reached"


def attempt_from_a_child(addresses):
    """The same attempts made by a new process, which inherits the confinement."""
    args = [f"{host}:{port}" for host, port in addresses]
    p = subprocess.run([sys.executable, "-I", "-c", _CHILD, *args], capture_output=True, text=True, timeout=30)
    return p.stdout.split("\n")[:-1] if p.returncode == 0 else [f"error the child failed: {p.stderr.strip()}"]


def hits():
    """How many connections have ever reached a synthetic endpoint. Any is a failure."""
    for s in _state["outside"]:
        while True:
            try:
                conn, _ = s.accept()
            except OSError:
                break
            conn.close()
            _state["hits"] += 1
    return _state["hits"]


def outside():
    """The synthetic endpoints: listening, and never to be reached."""
    return [s.getsockname() for s in _state["outside"]]


def enter():
    """Deny this process every TCP connection, and check that the denial holds. Call once,
    before any thread is started and before anything is executed."""
    if _state["confined"]:
        return
    if threading.active_count() != 1:
        raise ConfinementError("confinement must be entered before any other thread exists")
    libc = ctypes.CDLL(None, use_errno=True)
    abi = libc.syscall(_SYS_CREATE_RULESET, None, 0, 1)  # LANDLOCK_CREATE_RULESET_VERSION
    if abi < _NET_ABI:
        why = os.strerror(ctypes.get_errno()) if abi < 0 else f"its Landlock ABI is {abi}, older than {_NET_ABI}"
        raise ConfinementError(f"this kernel cannot confine network access ({why})")

    _state["outside"] = [_listen(host) for host in _OUTSIDE_ADDRESSES]

    # TCP connect is handled and no rule allows any port: every connection is refused.
    attr = struct.pack("QQ", 0, _CONNECT_TCP)
    fd = libc.syscall(_SYS_CREATE_RULESET, attr, len(attr), 0)
    if fd < 0:
        raise ConfinementError(f"landlock_create_ruleset: {os.strerror(ctypes.get_errno())}")
    if libc.prctl(_PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != 0:
        raise ConfinementError(f"prctl(NO_NEW_PRIVS): {os.strerror(ctypes.get_errno())}")
    if libc.syscall(_SYS_RESTRICT_SELF, fd, 0) != 0:
        raise ConfinementError(f"landlock_restrict_self: {os.strerror(ctypes.get_errno())}")
    os.close(fd)

    # Measured, not assumed: each endpoint is listening, so only the boundary can refuse it.
    seen = [attempt(a) for a in outside()] + attempt_from_a_child(outside())
    if seen != ["denied"] * (2 * len(_OUTSIDE_ADDRESSES)) or hits():
        raise ConfinementError(f"the boundary did not hold (attempts: {seen}; connections received: {hits()})")
    _state["confined"] = True
    atexit.register(_final_check)


def _final_check():
    reached = hits()
    for s in _state["outside"]:
        s.close()
    if reached:
        print(f"CONFINEMENT FAILED: {reached} connection(s) reached an endpoint outside the tests.", file=sys.stderr)
        os._exit(3)


def inside():
    return _state["confined"]


def require():
    """Refuse to go on unless this process is confined. Call before executing anything that
    could send a rig command."""
    if not inside():
        raise RuntimeError("this process is not network-confined: nothing that could reach a daemon is "
                           "executed (run the test file itself: python3 -I <file>)")


class Confinement(unittest.TestCase):
    """Add to a confined test file: the evidence that the boundary holds for this run. None of
    these tests executes the script under test."""

    def test_every_synthetic_endpoint_is_refused_by_the_kernel_at_either_address(self):
        self.assertTrue(inside())
        self.assertEqual([host for host, _ in outside()], list(_OUTSIDE_ADDRESSES))
        self.assertEqual([attempt(a) for a in outside()], ["denied", "denied"])
        self.assertEqual(hits(), 0)

    def test_a_child_process_is_refused_too(self):
        self.assertEqual(attempt_from_a_child(outside()), ["denied", "denied"])
        self.assertEqual(hits(), 0)

    def test_no_port_is_allowed_a_listener_opened_later_is_refused_at_either_address(self):
        # There is no allowance to outlive its fixture or to be taken over by another listener.
        for host in _OUTSIDE_ADDRESSES:
            with _listen(host) as later:
                self.assertEqual(attempt(later.getsockname()), "denied")
                self.assertEqual(attempt_from_a_child([later.getsockname()]), ["denied"])
                with self.assertRaises(BlockingIOError):  # nothing is waiting to be accepted
                    later.accept()


def main():
    """Confine this process, then run the calling file's tests. Never runs them unconfined."""
    try:
        enter()
    except ConfinementError as e:
        print(f"NOT RUN: {e}. These tests execute a script that can send a rig command, so they are never "
              "run without network confinement.", file=sys.stderr)
        sys.exit(2)
    unittest.main(module="__main__")
