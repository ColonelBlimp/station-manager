"""Tests for scripts/desktop-rpm-check.py (W-0027 D1 part 2):  python3 -I scripts/desktop_rpm_check_test.py

Synthetic manifests only: a valid package, then one mutation per rule, each of which must be
reported. The real artifact is checked by running the script on the built RPM.
"""
import copy
import importlib.util
import json
import os
import sys
import unittest

sys.dont_write_bytecode = True  # no __pycache__ beside the scripts

_here = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("chk", os.path.join(_here, "desktop-rpm-check.py"))
chk = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(chk)

LIB = "/usr/lib/station-manager-desktop"
APP = LIB + "/resources/app"
SRC = ["daemon.mjs", "main.mjs", "mode.mjs"]
DESKTOP = """[Desktop Entry]
Type=Application
Name=Station Manager
Comment=Amateur radio station manager
Exec=station-manager
Icon=station-manager-desktop
Terminal=false
Categories=Network;HamRadio;
StartupWMClass=station-manager-desktop
"""
PKG = {"name": "station-manager-desktop", "version": "2.0.0-alpha.5", "license": "GPL-3.0-only",
       "type": "module", "main": "src/main.mjs"}


def f(path, mode=0o100644, link=""):
    return {"path": path, "mode": mode, "user": "root", "group": "root", "link": link}


def valid():
    files = [
        f("/usr/bin/station-manager", 0o120777, LIB + "/station-manager-desktop"),
        f(LIB + "/station-manager-desktop", 0o100755),
        f(LIB + "/chrome-sandbox", 0o100755),
        f(LIB + "/chrome_crashpad_handler", 0o100755),
        f(LIB + "/libffmpeg.so", 0o100755),
        f(LIB + "/resources.pak"),
        f(APP, 0o40755),
        f(APP + "/src", 0o40755),
        f(APP + "/assets", 0o40755),
        f(APP + "/package.json"),
        f(APP + "/assets/icon.png"),
        f("/usr/share/applications/station-manager-desktop.desktop"),
        f("/usr/share/icons/hicolor/256x256/apps/station-manager-desktop.png"),
        f("/usr/share/icons/hicolor/scalable/apps/station-manager-desktop.svg"),
        f("/usr/share/licenses/station-manager-desktop/COPYING"),
        f("/usr/share/licenses/station-manager-desktop/LICENSE.electron"),
        f("/usr/share/licenses/station-manager-desktop/LICENSES.chromium.html"),
    ] + [f(APP + "/src/" + s) for s in SRC]
    return {
        "name": "station-manager-desktop", "version": "2.0.0~alpha.5", "release": "1",
        "arch": "x86_64",
        "requires": ["station-manager = 2.0.0~alpha.5-1", "libc.so.6()(64bit)",
                     "libgtk-3.so.0()(64bit)", "rpmlib(CompressedFileNames) <= 3.0.4-1"],
        "scripts": "",
        "files": files,
        "needed": {LIB + "/station-manager-desktop": ["libc.so.6", "libgtk-3.so.0", "libffmpeg.so"],
                   LIB + "/libffmpeg.so": ["libc.so.6"]},
        "sonames": {LIB + "/libffmpeg.so": "libffmpeg.so"},
        "package_json": json.dumps(PKG),
        "desktop": DESKTOP,
        "desktop_validate": "",
        "icon_png_size": (256, 256),
    }


EXPECT = {"version": "2.0.0~alpha.5", "src_files": SRC, "app_version": "2.0.0-alpha.5"}


def problems(m):
    return chk.problems(m, EXPECT)


def by(m, path):
    return next(x for x in m["files"] if x["path"] == path)


def without(m, path):
    m["files"] = [x for x in m["files"] if x["path"] != path]
    return m


class Check(unittest.TestCase):
    def assertReported(self, m, needle):
        p = problems(m)
        self.assertTrue(any(needle in x for x in p), f"not reported ({needle!r}): {p}")

    def test_valid_package_passes(self):
        self.assertEqual(problems(valid()), [])

    def test_identity(self):
        for key, value, needle in [("name", "electron", "name"), ("arch", "noarch", "arch"),
                                   ("version", "2.0.0~alpha.4", "version")]:
            m = valid()
            m[key] = value
            self.assertReported(m, needle)

    def test_exact_daemon_requirement(self):
        for req in ["station-manager", "station-manager >= 2.0.0~alpha.5",
                    "station-manager = 2.0.0~alpha.5", "station-manager = 2.0.0~alpha.4-1"]:
            m = valid()
            m["requires"] = [r for r in m["requires"] if not r.startswith("station-manager")] + [req]
            self.assertReported(m, "station-manager")
        m = valid()
        m["requires"] = [r for r in m["requires"] if not r.startswith("station-manager")]
        self.assertReported(m, "station-manager")

    def test_library_requirements_match_the_shipped_binaries(self):
        m = valid()
        m["requires"].remove("libgtk-3.so.0()(64bit)")
        self.assertReported(m, "libgtk-3.so.0")
        m = valid()
        m["requires"].append("libextra.so.1()(64bit)")
        self.assertReported(m, "libextra.so.1")
        m = valid()
        m["requires"].append("libffmpeg.so()(64bit)")  # bundled: never a system requirement
        self.assertReported(m, "libffmpeg.so")
        m = valid()
        m["requires"].append("gtk3")  # an unexplained requirement of any form
        self.assertReported(m, "gtk3")

    def test_no_scriptlets(self):
        m = valid()
        m["scripts"] = "postinstall scriptlet (using /bin/sh):\nsystemctl --user restart smd\n"
        self.assertReported(m, "scriptlet")

    def test_runtime_binary_is_renamed(self):
        m = without(valid(), LIB + "/station-manager-desktop")
        self.assertReported(m, "station-manager-desktop")
        m = valid()
        m["files"].append(f(LIB + "/electron", 0o100755))
        self.assertReported(m, "electron")

    def test_launcher(self):
        m = valid()
        by(m, "/usr/bin/station-manager")["link"] = LIB + "/electron"
        self.assertReported(m, "/usr/bin/station-manager")
        m = without(valid(), "/usr/bin/station-manager")
        self.assertReported(m, "/usr/bin/station-manager")

    def test_nothing_from_development(self):
        for extra in [APP + "/test/scenario-a1.mjs", APP + "/harness/server.mjs",
                      APP + "/node_modules/x/index.js", LIB + "/resources/default_app.asar",
                      APP + "/eslint.config.js", APP + "/package-lock.json"]:
            m = valid()
            m["files"].append(f(extra))
            self.assertReported(m, extra.rsplit("/", 1)[-1] if "default_app" in extra else extra)

    def test_development_directories_are_refused_too(self):
        for d in [APP + "/test", APP + "/harness", APP + "/node_modules", APP + "/src/nested"]:
            m = valid()
            m["files"].append(f(d, 0o40755))
            self.assertReported(m, d)

    def test_app_sources_complete_and_nothing_extra(self):
        m = without(valid(), APP + "/src/mode.mjs")
        self.assertReported(m, "mode.mjs")
        m = valid()
        m["files"].append(f(APP + "/src/dev-only.mjs"))
        self.assertReported(m, "dev-only.mjs")

    def test_production_package_json(self):
        for change, needle in [({"scripts": {"start": "electron ."}}, "scripts"),
                               ({"devDependencies": {"electron": "44"}}, "devDependencies"),
                               ({"dependencies": {"x": "1"}}, "dependencies"),
                               ({"main": "harness/server.mjs"}, "main"),
                               ({"version": "0.0.0"}, "version")]:
            m = valid()
            m["package_json"] = json.dumps({**PKG, **change})
            self.assertReported(m, needle)

    def test_sandbox_helper_not_setuid_and_modes(self):
        m = valid()
        by(m, LIB + "/chrome-sandbox")["mode"] = 0o104755
        self.assertReported(m, "setuid")
        m = valid()
        by(m, LIB + "/station-manager-desktop")["mode"] = 0o100644
        self.assertReported(m, "executable")
        m = valid()
        by(m, LIB + "/resources.pak")["mode"] = 0o100666
        self.assertReported(m, "writable")

    def test_ownership(self):
        m = valid()
        by(m, LIB + "/chrome_crashpad_handler")["user"] = "mveary"
        self.assertReported(m, "root")

    def test_desktop_entry(self):
        for old, new, needle in [("Name=Station Manager", "Name=Electron", "Name"),
                                 ("Exec=station-manager", "Exec=electron .", "Exec"),
                                 ("Terminal=false", "Terminal=true", "Terminal"),
                                 ("StartupWMClass=station-manager-desktop", "StartupWMClass=Electron",
                                  "StartupWMClass"),
                                 ("Icon=station-manager-desktop", "Icon=electron", "Icon")]:
            m = valid()
            m["desktop"] = m["desktop"].replace(old, new)
            self.assertReported(m, needle)
        m = valid()
        m["desktop_validate"] = "error: key \"Categories\" is invalid"
        self.assertReported(m, "desktop-file-validate")
        m = without(valid(), "/usr/share/applications/station-manager-desktop.desktop")
        self.assertReported(m, ".desktop")

    def test_icons_and_licences(self):
        for path in ["/usr/share/icons/hicolor/256x256/apps/station-manager-desktop.png",
                     "/usr/share/icons/hicolor/scalable/apps/station-manager-desktop.svg",
                     "/usr/share/licenses/station-manager-desktop/COPYING",
                     "/usr/share/licenses/station-manager-desktop/LICENSE.electron",
                     "/usr/share/licenses/station-manager-desktop/LICENSES.chromium.html"]:
            m = without(valid(), path)
            self.assertReported(m, path)
        m = valid()
        m["icon_png_size"] = (128, 128)
        self.assertReported(m, "256x256")

    def test_mutations_do_not_leak_between_cases(self):
        a = valid()
        b = copy.deepcopy(a)
        a["files"].append(f(APP + "/test/x.mjs"))
        self.assertEqual(problems(b), [])


if __name__ == "__main__":
    unittest.main()
