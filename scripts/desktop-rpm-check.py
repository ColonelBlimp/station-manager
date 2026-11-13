#!/usr/bin/env python3
"""Check the built desktop RPM's contents (W-0027 D1 part 2).

Usage: python3 -I scripts/desktop-rpm-check.py <rpm> --version <rpm-version> --app-version <version>

The artifact itself is inspected: its header (name, version, arch, requirements, scriptlets),
its file list with modes and owners, and its extracted contents (the production package.json,
the desktop entry, the menu group, the icon, and the libraries each shipped ELF file needs). Exit 0 with a
summary, or 1 with one line per problem. Tests: scripts/desktop_rpm_check_test.py.
"""
import argparse
import json
import os
import re
import struct
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET

NAME = "station-manager-desktop"
LIB = "/usr/lib/" + NAME
APP = LIB + "/resources/app"
BINARY = LIB + "/" + NAME
LAUNCHER = "/usr/bin/station-manager"
DESKTOP_FILE = "/usr/share/applications/" + NAME + ".desktop"
ICON_PNG = "/usr/share/icons/hicolor/256x256/apps/" + NAME + ".png"
ICON_SVG = "/usr/share/icons/hicolor/scalable/apps/" + NAME + ".svg"
LICENCES = ["/usr/share/licenses/" + NAME + "/" + n
            for n in ("COPYING", "LICENSE.electron", "LICENSES.chromium.html")]
EXECUTABLES = [BINARY, LIB + "/chrome-sandbox", LIB + "/chrome_crashpad_handler"]
DESKTOP_KEYS = {"Name": "Station Manager", "Exec": "station-manager", "Icon": NAME,
                "Terminal": "false", "StartupWMClass": NAME, "Type": "Application"}
# The Amateur Radio menu group (operator, 2026-11-12). Fedora defines none (measured on Fedora
# 44: no menu or directory file for it, and no *-menus package), so the package ships it the way
# Fedora's own *-menus packages do: a merged menu file and a directory file.
MENU_FILE = "/etc/xdg/menus/applications-merged/" + NAME + ".menu"
DIRECTORY_NAME = "station-manager-amateur-radio.directory"
DIRECTORY_FILE = "/usr/share/desktop-directories/" + DIRECTORY_NAME
DIRECTORY_KEYS = {"Type": "Directory", "Name": "Amateur Radio", "Icon": NAME}
GROUP = "Amateur Radio"
GROUP_CATEGORY = "HamRadio"
PKG_KEYS = {"name", "version", "description", "license", "type", "main"}


def _desktop_entry(text):
    keys, section = {}, None
    for line in text.splitlines():
        line = line.strip()
        if line.startswith("["):
            section = line
        elif section == "[Desktop Entry]" and "=" in line and not line.startswith("#"):
            k, v = line.split("=", 1)
            keys[k.strip()] = v.strip()
    return keys


def _children(e):
    """An element's children as (tag, text, element), ignoring comments."""
    return [(c.tag, (c.text or "").strip(), c) for c in e if isinstance(c.tag, str)]


def _rule(e):
    """A rule element's contents as sorted (tag, text) pairs; nested rules are kept visible."""
    return sorted((t, x if not len(c) else "(nested)") for t, x, c in _children(e))


def _shape(e):
    """An element as a comparable value: tag, attributes, text, the text after it, and its
    children's shapes in a fixed order. Comments and surrounding whitespace are dropped."""
    return (e.tag, tuple(sorted(e.attrib.items())), (e.text or "").strip(), (e.tail or "").strip(),
            tuple(sorted(_shape(c) for c in e if isinstance(c.tag, str))))


def _leaf(tag, text):
    return (tag, (), text, "", ())


def _node(tag, *children):
    return (tag, (), "", "", tuple(sorted(children)))


# The whole menu file, as the only shape the package may ship.
MENU_SHAPE = _node(
    "Menu",
    _leaf("Name", "Applications"),
    _node("Menu", _leaf("Name", GROUP), _leaf("Directory", DIRECTORY_NAME),
          _node("Include", _leaf("Category", GROUP_CATEGORY))),
    _node("Menu", _leaf("Name", "Internet"),
          _node("Exclude", _leaf("Filename", NAME + ".desktop"))),
)


def _menu_problems(text):
    """Every way the merged menu file differs from the one the package may ship.

    Exactly: the group, including the category the desktop entry carries; and the removal of
    Station Manager's own entry, by filename, from Internet. Removing by category would move
    every ham-radio application (operator ruling 2026-11-12: "only Station Manager"), and any
    other element would change a menu this package does not own.

    The specific findings below say what is wrong; the last word is the comparison of the
    whole file with MENU_SHAPE. A desktop combines sections of one name and may read a
    repeated name, an attribute or stray text its own way, so nothing this check does not
    look at is allowed to be there (codex-100: a repeated Internet section was read as its
    last occurrence only).
    """
    try:
        root = ET.fromstring(text or "")
    except ET.ParseError as e:
        return [f"menu: not well-formed XML ({e})"]
    if root.tag != "Menu":
        return [f"menu: the root element is <{root.tag}>, not <Menu>"]
    out = []
    menus = {}
    seen = {}
    for tag, value, e in _children(root):
        if tag == "Name":
            continue
        if tag != "Menu":
            out.append(f"menu: an element this package does not own: <{tag}>")
            continue
        name = next((x for t, x, _ in _children(e) if t == "Name"), "")
        seen[name] = seen.get(name, 0) + 1
        menus[name] = e
    for name in sorted(n for n, count in seen.items() if count > 1):
        out.append(f"menu: {name!r} appears {seen[name]} times; a desktop combines them, so "
                   f"each section may appear once")
    for name in sorted(set(menus) - {GROUP, "Internet"}):
        out.append(f"menu: another menu is changed: {name!r}")

    group = menus.get(GROUP)
    if group is None:
        out.append(f"menu: {GROUP!r} is not defined")
    else:
        parts = [(t, x) for t, x, _ in _children(group) if t not in ("Name", "Include")]
        if parts != [("Directory", DIRECTORY_NAME)]:
            out.append(f"menu: Directory and nothing else beside Name and Include is wanted in "
                       f"{GROUP!r}; found {parts}")
        includes = [_rule(e) for t, _, e in _children(group) if t == "Include"]
        if includes != [[("Category", GROUP_CATEGORY)]]:
            out.append(f"menu: Include must be exactly the category {GROUP_CATEGORY}; found {includes}")

    internet = menus.get("Internet")
    only = [("Filename", NAME + ".desktop")]
    if internet is None:
        out.append(f"menu: Internet has no rule removing {NAME}.desktop, so the application "
                   f"would stay under Internet")
    else:
        rules = [(t, _rule(e)) for t, _, e in _children(internet) if t != "Name"]
        if rules != [("Exclude", only)]:
            out.append(f"menu: Internet must only exclude {NAME}.desktop by filename "
                       f"(only Station Manager moves); found {rules}")
    if not out and _shape(root) != MENU_SHAPE:
        out.append("menu: the file is not exactly the shape the package may ship (a repeated "
                   "name, an attribute or stray text)")
    return out


def problems(m, expect):
    """Every way the manifest `m` differs from the D1 package contract."""
    out = []
    add = out.append
    files = {x["path"]: x for x in m["files"]}

    # Identity and the exact daemon version (operator ruling Q4).
    if m["name"] != NAME:
        add(f"name is {m['name']!r}, not {NAME!r}")
    if m["arch"] != "x86_64":
        add(f"arch is {m['arch']!r}, not 'x86_64'")
    if m["version"] != expect["version"]:
        add(f"version is {m['version']!r}, not {expect['version']!r}")
    exact = f"station-manager = {m['version']}-{m['release']}"
    daemon = [r for r in m["requires"] if r.split()[0] == "station-manager"]
    if daemon != [exact]:
        add(f"the station-manager requirement is {daemon}, not exactly [{exact!r}]")

    # System libraries: exactly the sonames the shipped ELF files need, minus those it ships.
    bundled = set(m["sonames"].values()) | {p.rsplit("/", 1)[-1] for p in files if p.startswith(LIB + "/")}
    needed = {so for libs in m["needed"].values() for so in libs} - bundled
    want = {f"{so}()(64bit)" for so in needed}
    have = {r for r in m["requires"]
            if not r.startswith("rpmlib(") and r.split()[0] != "station-manager"}
    for r in sorted(want - have):
        add(f"missing library requirement {r}")
    for r in sorted(have - want):
        add(f"unexplained requirement {r!r} (not needed by a shipped binary, or bundled)")

    if m["scripts"].strip():
        add("the package has a scriptlet; D1 ships none: " + m["scripts"].strip().splitlines()[0])

    # The runtime, renamed so app.isPackaged is true, and the launcher.
    if BINARY not in files:
        add(f"the runtime binary {BINARY} is missing")
    for p in files:
        if p.rsplit("/", 1)[-1] == "electron":
            add(f"an unrenamed runtime binary is shipped: {p}")
        if p.endswith("default_app.asar"):
            add(f"Electron's default app is shipped: {p} (a broken app would fall back to it)")
    link = files.get(LAUNCHER)
    if not link or (link["mode"] & 0o170000) != 0o120000 or link["link"] != BINARY:
        add(f"{LAUNCHER} must be a symlink to {BINARY}: {link}")

    # The app: only package.json, src/ and assets/ (an allowlist), and exactly the shell's sources.
    src = set()
    for p in files:
        if not p.startswith(APP + "/"):
            continue
        rel = p[len(APP) + 1:]
        if (files[p]["mode"] & 0o170000) == 0o040000 and rel in ("src", "assets"):
            continue  # the two directories themselves (nfpm's tree lists directories)
        if rel == "package.json" or re.fullmatch(r"assets/[\w.-]+", rel):
            continue
        if re.fullmatch(r"src/[\w-]+\.mjs", rel):
            src.add(rel[4:])
            continue
        add(f"a file outside the app's allowlist is shipped: {p}")
    for s in sorted(set(expect["src_files"]) - src):
        add(f"an app source is missing: src/{s}")
    for s in sorted(src - set(expect["src_files"])):
        add(f"an app source is shipped that the shell does not have: src/{s}")
    try:
        pkg = json.loads(m["package_json"])
    except (TypeError, ValueError):
        pkg = None
    if not isinstance(pkg, dict):
        add("the app's package.json is missing or not JSON")
    else:
        for k in sorted(set(pkg) - PKG_KEYS):
            add(f"the production package.json carries {k!r}")
        if pkg.get("main") != "src/main.mjs":
            add(f"the production package.json main is {pkg.get('main')!r}, not 'src/main.mjs'")
        if pkg.get("name") != NAME:
            add(f"the production package.json name is {pkg.get('name')!r}")
        if pkg.get("version") != expect["app_version"]:
            add(f"the production package.json version is {pkg.get('version')!r}, "
                f"not {expect['app_version']!r}")

    # Modes and owners.
    for p, x in files.items():
        kind = x["mode"] & 0o170000
        if x["user"] != "root" or x["group"] != "root":
            add(f"{p} is owned by {x['user']}:{x['group']}, not root:root")
        if x.get("config") != (p == MENU_FILE):
            add(f"{p} is {'' if x.get('config') else 'not '}a config file; "
                f"only {MENU_FILE} is one (it lives in /etc)")
        if kind == 0o120000:
            continue
        if x["mode"] & 0o6000:
            add(f"{p} is setuid or setgid ({oct(x['mode'])}); the sandbox uses user namespaces")
        if x["mode"] & 0o022:
            add(f"{p} is group- or world-writable ({oct(x['mode'])})")
    for p in EXECUTABLES:
        if p in files and not files[p]["mode"] & 0o111:
            add(f"{p} is not executable ({oct(files[p]['mode'])})")

    # The desktop entry, icons and licences.
    if DESKTOP_FILE not in files:
        add(f"the desktop entry {DESKTOP_FILE} is missing (.desktop)")
    else:
        keys = _desktop_entry(m["desktop"] or "")
        for k, v in DESKTOP_KEYS.items():
            if keys.get(k) != v:
                add(f"desktop entry {k}={keys.get(k)!r}, not {v!r}")
        if m["desktop_validate"].strip():
            add("desktop-file-validate: " + m["desktop_validate"].strip().splitlines()[0])
        if GROUP_CATEGORY not in [c for c in keys.get("Categories", "").split(";") if c]:
            add(f"desktop entry Categories={keys.get('Categories')!r} lacks {GROUP_CATEGORY}, "
                f"the category the {GROUP} group includes")

    # The Amateur Radio menu group.
    for p in [MENU_FILE, DIRECTORY_FILE]:
        if p not in files:
            add(f"missing {p}")
    if DIRECTORY_FILE in files:
        keys = _desktop_entry(m.get("directory") or "")
        for k, v in DIRECTORY_KEYS.items():
            if keys.get(k) != v:
                add(f"directory entry {k}={keys.get(k)!r}, not {v!r}")
    if MENU_FILE in files:
        out.extend(_menu_problems(m.get("menu")))
    for p in [ICON_PNG, ICON_SVG] + LICENCES:
        if p not in files:
            add(f"missing {p}")
    if ICON_PNG in files and tuple(m["icon_png_size"] or ()) != (256, 256):
        add(f"{ICON_PNG} is {m['icon_png_size']}, not 256x256")
    return out


# ─── Reading the artifact ─────────────────────────────────────────────────────

def _run(*cmd, **kw):
    return subprocess.run(cmd, check=True, capture_output=True, text=True, **kw).stdout


def _png_size(path):
    with open(path, "rb") as fh:
        head = fh.read(24)
    if head[:8] != b"\x89PNG\r\n\x1a\n":
        return None
    return struct.unpack(">II", head[16:24])


def _elf_dynamic(path):
    """(soname, [needed]) from readelf, or None when the file is not ELF."""
    with open(path, "rb") as fh:
        if fh.read(4) != b"\x7fELF":
            return None
    text = _run("readelf", "-d", "-W", path)
    needed = re.findall(r"\(NEEDED\)\s+Shared library: \[([^\]]+)\]", text)
    soname = re.findall(r"\(SONAME\)\s+Library soname: \[([^\]]+)\]", text)
    return (soname[0] if soname else None), needed


def inspect(rpm):
    q = lambda fmt: _run("rpm", "-qp", "--qf", fmt, rpm)  # noqa: E731
    m = {
        "name": q("%{NAME}"), "version": q("%{VERSION}"), "release": q("%{RELEASE}"),
        "arch": q("%{ARCH}"),
        "requires": [r for r in _run("rpm", "-qp", "--requires", rpm).splitlines() if r.strip()],
        "scripts": _run("rpm", "-qp", "--scripts", rpm),
        "files": [], "needed": {}, "sonames": {},
        "package_json": None, "desktop": None, "desktop_validate": "", "icon_png_size": None,
        "menu": None, "directory": None,
    }
    # FILEFLAGS bit 0 is RPMFILE_CONFIG.
    for line in q("[%{FILEMODES}\t%{FILEUSERNAME}\t%{FILEGROUPNAME}\t%{FILEFLAGS}\t%{FILELINKTOS}\t%{FILENAMES}\n]").splitlines():
        mode, user, group, flags, link, path = line.split("\t", 5)
        m["files"].append({"path": path, "mode": int(mode) & 0xFFFF, "user": user, "group": group,
                           "link": link, "config": bool(int(flags) & 1)})
    with tempfile.TemporaryDirectory(prefix="sm-desktop-rpm-check-") as root:
        cpio = subprocess.Popen(["rpm2cpio", rpm], stdout=subprocess.PIPE)
        subprocess.run(["cpio", "-idm", "--quiet", "--no-absolute-filenames"], cwd=root,
                       stdin=cpio.stdout, check=True)
        cpio.stdout.close()
        if cpio.wait() != 0:
            raise RuntimeError("rpm2cpio failed")
        at = lambda p: os.path.join(root, p.lstrip("/"))  # noqa: E731
        for x in m["files"]:
            p = at(x["path"])
            if (x["mode"] & 0o170000) == 0o100000 and os.path.isfile(p):
                dyn = _elf_dynamic(p)
                if dyn:
                    soname, needed = dyn
                    m["needed"][x["path"]] = needed
                    if soname:
                        m["sonames"][x["path"]] = soname
        if os.path.isfile(at(APP + "/package.json")):
            with open(at(APP + "/package.json"), encoding="utf-8") as fh:
                m["package_json"] = fh.read()
        if os.path.isfile(at(DESKTOP_FILE)):
            with open(at(DESKTOP_FILE), encoding="utf-8") as fh:
                m["desktop"] = fh.read()
            r = subprocess.run(["desktop-file-validate", at(DESKTOP_FILE)],
                               capture_output=True, text=True)
            m["desktop_validate"] = (r.stdout + r.stderr) if (r.returncode or r.stdout.strip()) else ""
        if os.path.isfile(at(ICON_PNG)):
            m["icon_png_size"] = _png_size(at(ICON_PNG))
        for key, path in (("menu", MENU_FILE), ("directory", DIRECTORY_FILE)):
            if os.path.isfile(at(path)):
                with open(at(path), encoding="utf-8") as fh:
                    m[key] = fh.read()
    return m


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("rpm")
    ap.add_argument("--version", required=True, help="the expected RPM Version")
    ap.add_argument("--app-version", required=True, help="the expected readable version")
    ap.add_argument("--src", default=os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                                  "..", "desktop", "src"))
    a = ap.parse_args(argv)
    src = sorted(n for n in os.listdir(a.src) if n.endswith(".mjs"))
    m = inspect(a.rpm)
    found = problems(m, {"version": a.version, "app_version": a.app_version, "src_files": src})
    for p in found:
        print("FAIL " + p)
    if found:
        print(f"desktop RPM check FAILED: {len(found)} problem(s) in {a.rpm}")
        return 1
    libs = sorted(r for r in m["requires"] if r.endswith("()(64bit)"))
    print(f"desktop RPM check passed: {m['name']}-{m['version']}-{m['release']}.{m['arch']}, "
          f"{len(m['files'])} files, {os.path.getsize(a.rpm) // (1024 * 1024)} MiB, "
          f"{len(libs)} library requirements, "
          f"{[r for r in m['requires'] if r.startswith('station-manager')][0]}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
