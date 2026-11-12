#!/usr/bin/env python3
"""Production smoke check of the desktop app RPM (W-0027 D1 part 3, brief item 9).

Usage:
  python3 -I scripts/desktop-smoke.py <rpm>                  the shipped artifact, every selector
  python3 -I scripts/desktop-smoke.py <rpm> --reversions     isolated review copies, each must fail

The RPM is unpacked into a temporary directory and its renamed runtime is launched directly,
once for each development selector (SM_DESKTOP_SCENARIO = a1, c1, c1d, c1d-shell, second), each
with a hostile SM_DESKTOP_USERDATA. Each launch runs from an unrelated directory, on this
script's own Xvfb, with an empty PATH (no Node, no checkout), an isolated HOME, a synthetic
config.json on a free port, and no daemon. An installed app must ignore the selectors and show
its waiting page in a sandboxed renderer. The verdict (judge) is pure and unit-tested:
scripts/desktop_smoke_test.py.

Each launch has an overall deadline and a bounded settle window; the script owns every process
it starts (its Xvfb, and each launch in its own session) and reaps them, then checks that none
is left. It never touches the station: no daemon is started and only temporary paths are used.
On Ubuntu runners, unprivileged user namespaces must be allowed first (CI does that).
"""
import argparse
import hashlib
import json
import os
import re
import select
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time

NAME = "station-manager-desktop"
LIB = "usr/lib/" + NAME
PREFIX = NAME + ": "
SELECTORS = ["a1", "c1", "c1d", "c1d-shell", "second"]
DEADLINE_S = 45  # one launch, from start to verdict
SETTLE_S = 3  # after the waiting page loads: nothing may fail or go away meanwhile
REAP_S = 10
MAX_DEPTH = 32  # renderer to main process (intermediate zygotes allowed)


# ─── The verdict (pure) ───────────────────────────────────────────────────────

def _trace(text):
    return [line[len(PREFIX):] for line in text.splitlines() if line.startswith(PREFIX)]


def judge(run):
    """Every way one launch's observations differ from the installed app's contract.

    run: binary, trace (the launch's stderr), main (pid), procs ({pid: {ppid, argv, seccomp,
    ns_user, ns_pid}}, read from /proc while the launch ran; a pid absent is uninspectable),
    alive, config_unchanged, hostile_exists, profile_exists, leftover (pids after the reap).
    """
    out = []
    add = out.append
    lines = _trace(run["trace"])
    procs = run["procs"]

    if not any(x.startswith("waiting: ") for x in lines):
        add("the trace never reported 'waiting:' (the discovery state)")
    for x in lines:
        if x.startswith("load failed") or x.startswith("renderer gone"):
            add(f"the trace reports a failure: {x!r}")
    loads = [x for x in lines if x.startswith("loaded ")]
    waiting = [x for x in loads if re.fullmatch(r"loaded waiting page renderer \d+", x)]
    if not waiting:
        add("the trace never reported 'loaded waiting page'")
    elif loads[-1] != waiting[-1]:
        add(f"the last load is not the waiting page: {loads[-1]!r}")

    main = procs.get(run["main"])
    if main is None:
        add(f"the main process {run['main']} could not be inspected")
    elif not main["argv"] or main["argv"][0] != run["binary"]:
        add(f"the main process {run['main']} is not {run['binary']}: {main['argv'][:1]}")

    if waiting:
        rpid = int(waiting[-1].rsplit(" ", 1)[1])
        r = procs.get(rpid)
        if r is None:
            add(f"renderer {rpid} named by the trace is missing, vanished or uninspectable")
        else:
            if "--type=renderer" not in r["argv"]:
                add(f"process {rpid} named by the trace is not a --type=renderer process")
            if not r["argv"] or r["argv"][0] != run["binary"]:
                add(f"renderer {rpid} does not run the binary {run['binary']}: {r['argv'][:1]}")
            seen, p, ok = set(), r["ppid"], False
            while p not in seen and len(seen) < MAX_DEPTH:
                if p == run["main"]:
                    ok = True
                    break
                seen.add(p)
                q = procs.get(p)
                if q is None:
                    break
                p = q["ppid"]
            if not ok:
                add(f"renderer {rpid} is not an inspectable descendant of the launch {run['main']}")
            if r["seccomp"] != 2:
                add(f"renderer {rpid} Seccomp is {r['seccomp']!r}, not 2 (filter mode)")
            if main is not None:
                for key, what in (("ns_user", "user namespace"), ("ns_pid", "PID namespace")):
                    if r[key] is None or main[key] is None or r[key] == main[key]:
                        add(f"renderer {rpid} {what} {r[key]!r} is not its own "
                            f"(main {main[key]!r})")

    if not run["alive"]:
        add("the app is not running after the settle window (it exited)")
    if not run["config_unchanged"]:
        add("the synthetic config.json changed")
    if run["hostile_exists"]:
        add("the hostile SM_DESKTOP_USERDATA directory was created")
    if not run["profile_exists"]:
        add("no profile under the isolated HOME's XDG config (the installed app's own)")
    if run["leftover"]:
        add(f"processes left running after the reap: {run['leftover']}")
    return out


# ─── Observing a launch ───────────────────────────────────────────────────────

def split_cmdline(raw):
    """argv from /proc/<pid>/cmdline. Chromium's child processes rewrite their title as one
    space-joined string (no NULs), so a single element is split on spaces; launch() refuses a
    root whose path has a space, so the binary's path stays one element."""
    argv = [a.decode(errors="replace") for a in raw.split(b"\0") if a]
    if len(argv) == 1 and " " in argv[0]:
        argv = argv[0].split(" ")
    return argv


def read_proc(pid):
    """{ppid, argv, seccomp, ns_user, ns_pid} for pid, or None if any part cannot be read."""
    try:
        with open(f"/proc/{pid}/status") as fh:
            status = fh.read()
        with open(f"/proc/{pid}/cmdline", "rb") as fh:
            argv = split_cmdline(fh.read())
        ppid = int(re.search(r"^PPid:\s+(\d+)", status, re.M).group(1))
        m = re.search(r"^Seccomp:\s+(\d+)", status, re.M)
        return {"ppid": ppid, "argv": argv, "seccomp": int(m.group(1)) if m else None,
                "ns_user": os.readlink(f"/proc/{pid}/ns/user"),
                "ns_pid": os.readlink(f"/proc/{pid}/ns/pid")}
    except (OSError, AttributeError, ValueError):
        return None


def snapshot(main, trace_text):
    """The main process, and each renderer the trace names with its ancestors."""
    procs = {}
    todo = [main] + [int(x.rsplit(" ", 1)[1]) for x in _trace(trace_text)
                     if re.fullmatch(r"loaded .* renderer \d+", x)]
    for pid in todo:
        for _ in range(MAX_DEPTH):
            if pid in procs or pid <= 1:
                break
            info = read_proc(pid)
            if info is None:
                break
            procs[pid] = info
            pid = info["ppid"]
    return procs


def owned(root):
    """Pids whose argv[0] is under this unpacked root: every process a launch of it started."""
    return owned_under(os.path.join(root, LIB))


def owned_under(directory):
    """Pids whose argv[0] is under directory (one root's runtime, or the whole run's work
    directory, which holds the original root and every review copy)."""
    lib = directory.rstrip("/") + "/"
    pids = []
    for d in os.listdir("/proc"):
        if not d.isdigit():
            continue
        try:
            with open(f"/proc/{d}/cmdline", "rb") as fh:
                argv = split_cmdline(fh.read())
            argv0 = argv[0] if argv else ""
        except OSError:
            continue
        if argv0.startswith(lib):
            pids.append(int(d))
    return pids


def reap(proc, root):
    """Stop the launch and everything it started; return what is still running."""
    for sig, wait in ((signal.SIGTERM, REAP_S / 2), (signal.SIGKILL, REAP_S / 2)):
        try:
            os.killpg(proc.pid, sig)
        except ProcessLookupError:
            pass
        for pid in owned(root):
            try:
                os.kill(pid, sig)
            except ProcessLookupError:
                pass
        end = time.monotonic() + wait
        while time.monotonic() < end:
            if proc.poll() is not None and not owned(root):
                return []
            time.sleep(0.1)
    proc.poll()
    return owned(root)


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _xvfb_argv(fd):
    return ["Xvfb", "-displayfd", str(fd), "-nolisten", "tcp", "-screen", "0", "1280x800x24"]


class Xvfb:
    """This script's own X server, on a display it chooses itself. Its start is bounded: the
    display is awaited with select() against the deadline, never a blocking read (codex-081),
    and every unsuccessful start closes both descriptors and kills and waits for the server."""

    def __init__(self, cmd=_xvfb_argv, timeout=15):
        self.cmd, self.timeout, self.proc = cmd, timeout, None

    def __enter__(self):
        r, w = os.pipe()
        try:
            try:
                self.proc = subprocess.Popen(self.cmd(w), pass_fds=(w,),
                                             stdout=subprocess.DEVNULL,
                                             stderr=subprocess.DEVNULL, start_new_session=True)
            finally:
                os.close(w)
            end, buf = time.monotonic() + self.timeout, b""
            while not buf.endswith(b"\n"):
                left = end - time.monotonic()
                if left <= 0 or not select.select([r], [], [], left)[0]:
                    raise RuntimeError(f"Xvfb reported no display within {self.timeout} s")
                chunk = os.read(r, 16)
                if not chunk:
                    raise RuntimeError("Xvfb exited without reporting a display")
                buf += chunk
            if not buf.strip().isdigit():
                raise RuntimeError(f"Xvfb reported {buf!r}, not a display number")
            self.display = ":" + buf.strip().decode()
            return self
        except BaseException:
            self.__exit__()
            raise
        finally:
            os.close(r)

    def __exit__(self, *exc):
        if self.proc is None:
            return
        try:
            os.killpg(self.proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        self.proc.wait(timeout=REAP_S)


def launch(root, selector, work, display, args=()):
    """One launch of the unpacked runtime; the observations judge() reads."""
    binary = os.path.join(root, LIB, NAME)
    if " " in binary:
        raise RuntimeError(f"the unpacked root must have no space in its path: {root}")
    run_dir = tempfile.mkdtemp(prefix=f"{selector}-", dir=work)
    home, wd, cwd, path = (os.path.join(run_dir, d) for d in ("home", "wd", "cwd", "empty-path"))
    for d in (home, wd, cwd, path):
        os.mkdir(d, 0o700)
    hostile = os.path.join(run_dir, "hostile-profile")
    config = os.path.join(wd, "config.json")
    with open(config, "w") as fh:
        json.dump({"server": {"app_port": free_port()}}, fh)
    os.chmod(config, 0o600)
    before = hashlib.sha256(open(config, "rb").read()).hexdigest()
    env = {"PATH": path, "HOME": home, "XDG_CONFIG_HOME": os.path.join(home, ".config"),
           "XDG_CACHE_HOME": os.path.join(home, ".cache"), "DISPLAY": display,
           "SM_WORKING_DIR": wd, "SM_DESKTOP_SCENARIO": selector,
           "SM_DESKTOP_USERDATA": hostile}
    if shutil.which("node", path=env["PATH"]):
        raise RuntimeError("node is reachable on the launch PATH")
    log = os.path.join(run_dir, "stderr.log")
    with open(log, "wb") as out:
        proc = subprocess.Popen([binary, *args], cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                stdout=out, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        start = time.monotonic()
        read = lambda: open(log, encoding="utf-8", errors="replace").read()  # noqa: E731
        while time.monotonic() - start < DEADLINE_S - SETTLE_S:
            lines = _trace(read())
            if (proc.poll() is not None
                    or any(re.fullmatch(r"loaded waiting page renderer \d+", x) for x in lines)
                    or any(x.startswith(("load failed", "renderer gone")) for x in lines)):
                break
            time.sleep(0.2)
        time.sleep(SETTLE_S)
        trace = read()
        procs = snapshot(proc.pid, trace)
        alive = proc.poll() is None
    finally:
        leftover = reap(proc, root)
    return {
        "binary": binary, "trace": trace, "main": proc.pid, "procs": procs, "alive": alive,
        "config_unchanged": hashlib.sha256(open(config, "rb").read()).hexdigest() == before,
        "hostile_exists": os.path.lexists(hostile),
        "profile_exists": os.path.isdir(os.path.join(home, ".config", NAME)),
        "leftover": leftover, "log": log,
    }


def unpack(rpm, dest):
    os.makedirs(dest)
    cpio = subprocess.Popen(["rpm2cpio", rpm], stdout=subprocess.PIPE)
    subprocess.run(["cpio", "-idm", "--quiet", "--no-absolute-filenames"], cwd=dest,
                   stdin=cpio.stdout, check=True)
    cpio.stdout.close()
    if cpio.wait() != 0:
        raise RuntimeError("rpm2cpio failed")
    if not os.access(os.path.join(dest, LIB, NAME), os.X_OK):
        raise RuntimeError(f"no executable {LIB}/{NAME} in {rpm}")


def smoke(root, selectors, work, display, quiet=False, args=()):
    """{selector: problems} for one unpacked root."""
    results = {}
    for s in selectors:
        run = launch(root, s, work, display, args)
        results[s] = judge(run)
        if not quiet:
            rp = [x for x in _trace(run["trace"]) if x.startswith("loaded ")]
            print(f"[{s}] {'ok' if not results[s] else 'FAIL'}: "
                  f"{rp[-1] if rp else 'no load traced'}; main {run['main']}")
            for p in results[s]:
                print(f"[{s}]   FAIL {p}")
            if results[s]:
                print(f"[{s}]   log: {run['log']}")
    return results


# ─── Reversions: isolated review copies of the unpacked artifact ──────────────

APP = LIB + "/resources/app/src/"
# Any of the sandbox diagnostics is the evidence for a sandbox reversion. Where the launching
# environment already applies a seccomp filter, every process shows Seccomp 2, unsandboxed or
# not, and only the namespaces tell (clean-room review of 8c667809).
SANDBOX = ("Seccomp", "user namespace", "PID namespace")
LEFTOVER = "processes left running after the reap"
REVERSIONS = [
    # (name, file, anchor, replacement, a fragment the verdict must report)
    ("wrong document", "waiting.mjs",
     "return remember('data:text/html;charset=utf-8,' + encodeURIComponent(html));",
     "remember('data:text/html;charset=utf-8,' + encodeURIComponent(html));"
     " return 'data:text/html;charset=utf-8,' + encodeURIComponent(html + ' ');",
     "never reported 'loaded waiting page'"),
    ("failing waiting-page load", "waiting.mjs",
     "return remember('data:text/html;charset=utf-8,' + encodeURIComponent(html));",
     "return remember('file:///nonexistent/sm-smoke-reversion.html');",
     "load failed"),
    ("sandbox: false", "window.mjs", "sandbox: true,", "sandbox: false,", SANDBOX),
    # Part 1's R10: without the window's trace there is no evidence, so the run must fail
    # (bounded by the deadline), never pass for want of a failure line.
    ("window trace removed", "window.mjs", "traceWindow(win.webContents, originOf);",
     "void originOf;", "never reported 'loaded waiting page'"),
]
# The switch itself, on the launch's command line. Appended from main.mjs instead
# (app.commandLine.appendSwitch), the browser and its children disagree and the renderer
# crashes on /dev/shm (measured 2026-11-12): a different failure from the one claimed.
ARG_REVERSIONS = [("--no-sandbox", ["--no-sandbox"], SANDBOX)]


def review_copy(root, dest):
    """A hard-linked copy: a reversion replaces its one file, never the shared original."""
    shutil.copytree(root, dest, symlinks=True, copy_function=os.link)


def revert(copy_root, name, anchor, replacement):
    path = os.path.join(copy_root, APP, name)
    with open(path, encoding="utf-8") as fh:
        text = fh.read()
    if text.count(anchor) != 1 or not replacement or replacement == anchor:
        raise RuntimeError(f"reversion anchor not unique in {name}: {anchor!r}")
    os.unlink(path)  # break the hard link first
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(text.replace(anchor, replacement))
    with open(path, encoding="utf-8") as fh:
        if replacement not in fh.read():
            raise RuntimeError(f"reversion not applied to {name}")


def caught(got, needles):
    """The problems that show this reversion's own reason, or [] when it is not caught. A
    launch whose processes survived the reap never counts as caught, whatever else it shows."""
    if isinstance(needles, str):
        needles = (needles,)
    if any(LEFTOVER in p for p in got):
        return []
    return [p for p in got if any(n in p for n in needles)]


def reversions(root, work, display):
    failed = []
    control = os.path.join(work, "review-control")
    review_copy(root, control)
    got = smoke(control, ["a1"], work, display, quiet=True)["a1"]
    print(f"[control] {'ok' if not got else 'FAIL'}: an unchanged review copy passes")
    if got:
        failed.append(("control", got))
    for i, (title, name, anchor, replacement, needle) in enumerate(REVERSIONS, 1):
        dest = os.path.join(work, f"review-{i}")
        review_copy(root, dest)
        revert(dest, name, anchor, replacement)
        got = smoke(dest, ["a1"], work, display, quiet=True)["a1"]
        hit = caught(got, needle)
        print(f"[reversion {i}: {title}] {'fails as required' if hit else 'NOT CAUGHT'}: "
              f"{'; '.join(got)}")
        if not hit:
            failed.append((title, got))
    for j, (title, args, needle) in enumerate(ARG_REVERSIONS, len(REVERSIONS) + 1):
        got = smoke(control, ["a1"], work, display, quiet=True, args=args)["a1"]
        hit = caught(got, needle)
        print(f"[reversion {j}: {title}] {'fails as required' if hit else 'NOT CAUGHT'}: "
              f"{'; '.join(got)}")
        if not hit:
            failed.append((title, got))
    return failed


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("rpm")
    ap.add_argument("--reversions", action="store_true",
                    help="run the reversion proofs on isolated review copies instead")
    ap.add_argument("--selectors", default=",".join(SELECTORS))
    a = ap.parse_args(argv)
    work = tempfile.mkdtemp(prefix="sm-desktop-smoke-")
    root = os.path.join(work, "root")
    # The run directory is removed only after a passing run whose processes are confirmed gone.
    # A failed check or any exception (unpack, Xvfb, a launch's observation or reap) keeps it,
    # with its logs and anything whose termination is unconfirmed, and names it (codex-081).
    # The status is decided only after the cleanup is confirmed (codex-082): a process of this
    # root still running turns a pass into a failure.
    try:
        rc = _run(a, root, work)
    except BaseException:
        print(f"desktop smoke: the run raised an exception; the run directory is kept: {work}",
              file=sys.stderr)
        raise
    # The whole work directory: the original root and every review copy (clean-room review
    # of 8c667809), not only the root the smoke itself launched.
    survivors = owned_under(work)
    if rc == 0 and not survivors:
        shutil.rmtree(work)  # exactly the directory this run made
        return 0
    if survivors:
        print(f"desktop smoke: processes of this run are still running: {survivors}",
              file=sys.stderr)
    print(f"desktop smoke: a check failed; the run directory is kept: {work}", file=sys.stderr)
    return rc or 1


def _run(a, root, work):
    unpack(os.path.abspath(a.rpm), root)
    with Xvfb() as x:
        if a.reversions:
            failed = reversions(root, work, x.display)
            if failed:
                print(f"desktop smoke reversions FAILED: {[t for t, _ in failed]}")
                return 1
            print(f"desktop smoke reversions passed: the control passes and all "
                  f"{len(REVERSIONS) + len(ARG_REVERSIONS)} reversions fail")
            return 0
        results = smoke(root, a.selectors.split(","), work, x.display)
    bad = [s for s, p in results.items() if p]
    if bad:
        print(f"desktop smoke check FAILED for {bad}")
        return 1
    print(f"desktop smoke check passed: {len(results)} launches of the installed runtime "
          f"({', '.join(results)}), each waiting in a sandboxed renderer")
    return 0

if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
