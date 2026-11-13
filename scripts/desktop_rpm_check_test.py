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
MENU_FILE = "/etc/xdg/menus/applications-merged/station-manager-desktop.menu"
DIRECTORY_FILE = "/usr/share/desktop-directories/station-manager-amateur-radio.directory"
MENU = """<!DOCTYPE Menu PUBLIC "-//freedesktop//DTD Menu 1.0//EN"
 "http://www.freedesktop.org/standards/menu-spec/menu-1.0.dtd">
<!-- a comment -->
<Menu>
  <Name>Applications</Name>
  <Menu>
    <Name>Amateur Radio</Name>
    <Directory>station-manager-amateur-radio.directory</Directory>
    <Include>
      <Category>HamRadio</Category>
    </Include>
  </Menu>
  <Menu>
    <Name>Internet</Name>
    <Exclude>
      <Filename>station-manager-desktop.desktop</Filename>
    </Exclude>
  </Menu>
</Menu>
"""
DIRECTORY = """[Desktop Entry]
Type=Directory
Name=Amateur Radio
Icon=station-manager-desktop
"""


def f(path, mode=0o100644, link="", config=False):
    return {"path": path, "mode": mode, "user": "root", "group": "root", "link": link,
            "config": config}


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
        f(MENU_FILE, config=True),
        f(DIRECTORY_FILE),
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
        "menu": MENU,
        "directory": DIRECTORY,
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

    # The Amateur Radio menu group (operator, 2026-11-12). Fedora defines none, so the package
    # ships it the way Fedora's own *-menus packages do: a merged menu file and a directory
    # file. Only Station Manager's own entry is removed from Internet.

    def test_menu_group_files(self):
        for path in [MENU_FILE, DIRECTORY_FILE]:
            self.assertReported(without(valid(), path), path)

    def test_menu_file_is_the_packages_only_config_file(self):
        m = valid()
        by(m, MENU_FILE)["config"] = False
        self.assertReported(m, "config")
        m = valid()
        by(m, DIRECTORY_FILE)["config"] = True
        self.assertReported(m, "config")

    def test_desktop_entry_is_in_the_category_the_group_includes(self):
        for cats in ["Network;", "Network;Utility;", "Network;XHamRadio;"]:
            m = valid()
            m["desktop"] = m["desktop"].replace("Categories=Network;HamRadio;", "Categories=" + cats)
            self.assertReported(m, "HamRadio")

    def test_directory_entry(self):
        for old, new, needle in [("Name=Amateur Radio", "Name=Ham", "Name"),
                                 ("Type=Directory", "Type=Application", "Type"),
                                 ("Icon=station-manager-desktop", "Icon=electron", "Icon")]:
            m = valid()
            m["directory"] = m["directory"].replace(old, new)
            self.assertReported(m, "directory entry " + needle)
        m = valid()
        m["directory"] = None
        self.assertReported(m, "directory entry")

    def test_menu_defines_the_group(self):
        for old, new, needle in [
            ("<Name>Amateur Radio</Name>", "<Name>Ham Radio</Name>", "'Amateur Radio' is not defined"),
            ("<Directory>station-manager-amateur-radio.directory</Directory>",
             "<Directory>other.directory</Directory>", "Directory"),
            ("<Directory>station-manager-amateur-radio.directory</Directory>", "", "Directory"),
            ("<Category>HamRadio</Category>", "<Category>Network</Category>", "Include"),
            ("<Category>HamRadio</Category>",
             "<Category>HamRadio</Category><Category>Network</Category>", "Include"),
            ("<Include>", "<Exclude>", "Include"),
        ]:
            m = valid()
            m["menu"] = m["menu"].replace(old, new).replace(
                "</Include>", "</Exclude>" if new == "<Exclude>" else "</Include>", 1)
            self.assertReported(m, "menu: " + needle)

    def test_menu_removes_only_station_manager_from_internet(self):
        entry = "<Filename>station-manager-desktop.desktop</Filename>"
        for new, why in [
            ("<Category>HamRadio</Category>", "every ham-radio application"),
            (entry + "<Filename>wsjtx.desktop</Filename>", "another application too"),
            ("<Filename>wsjtx.desktop</Filename>", "another application instead"),
            (entry + "<Category>Network</Category>", "a category as well"),
            ("", "nothing"),
        ]:
            with self.subTest(why=why):
                m = valid()
                m["menu"] = m["menu"].replace(entry, new)
                self.assertReported(m, "menu: Internet")
        # No Internet rule at all: the application would stay under Internet.
        m = valid()
        start = m["menu"].index("  <Menu>\n    <Name>Internet</Name>")
        m["menu"] = m["menu"][:start] + "</Menu>\n"
        self.assertReported(m, "menu: Internet")
        # An Include would ADD applications to Internet.
        m = valid()
        m["menu"] = m["menu"].replace("</Exclude>", "</Exclude><Include><All/></Include>")
        self.assertReported(m, "menu: Internet")

    def test_menu_touches_nothing_else(self):
        other = ("  <Menu>\n    <Name>Multimedia</Name>\n    <Exclude><Category>HamRadio</Category>"
                 "</Exclude>\n  </Menu>\n</Menu>\n")
        m = valid()
        m["menu"] = m["menu"][:m["menu"].rindex("</Menu>")] + other
        self.assertReported(m, "menu: another menu")
        m = valid()
        m["menu"] = m["menu"].replace("<Name>Applications</Name>",
                                      "<Name>Applications</Name><DefaultAppDirs/>")
        self.assertReported(m, "menu: ")
        # A nested menu inside the group is another menu too.
        m = valid()
        m["menu"] = m["menu"].replace("</Include>", "</Include><Menu><Name>More</Name></Menu>")
        self.assertReported(m, "menu: ")

    def test_menu_sections_are_not_repeated(self):
        # A desktop COMBINES sections of one name (menu spec, merge algorithm), so an earlier
        # Internet section's rule applies as well as the expected one's (codex-100).
        expected = "  <Menu>\n    <Name>Internet</Name>"
        earlier = ("  <Menu>\n    <Name>Internet</Name>\n    <Exclude><Filename>wsjtx.desktop"
                   "</Filename></Exclude>\n  </Menu>\n")
        m = valid()
        self.assertEqual(m["menu"].count(expected), 1)
        m["menu"] = m["menu"].replace(expected, earlier + expected)
        self.assertReported(m, "menu: 'Internet' appears 2 times")
        # The same after the expected section, and for the group.
        m = valid()
        m["menu"] = m["menu"][:m["menu"].rindex("</Menu>")] + earlier + "</Menu>\n"
        self.assertReported(m, "menu: 'Internet' appears 2 times")
        group = "  <Menu>\n    <Name>Amateur Radio</Name>"
        more = ("  <Menu>\n    <Name>Amateur Radio</Name>\n    <Include><Category>Network"
                "</Category></Include>\n  </Menu>\n")
        m = valid()
        m["menu"] = m["menu"].replace(group, more + group)
        self.assertReported(m, "menu: 'Amateur Radio' appears 2 times")

    def test_menu_is_exactly_the_shape_shipped(self):
        # Anything a desktop could read differently from this check is refused: a second name
        # for a section, an attribute, stray text, a repeated root name.
        for old, new, why in [
            ("<Name>Internet</Name>", "<Name>Internet</Name><Name>Multimedia</Name>", "a second name"),
            ("<Name>Amateur Radio</Name>", "<Name>Amateur Radio</Name><Name>Internet</Name>",
             "a second name for the group"),
            ("<Name>Applications</Name>", "<Name>Applications</Name><Name>Other</Name>",
             "a second root name"),
            ("<Name>Applications</Name>", "<Name>Other</Name>", "another root name"),
            ("<Exclude>", '<Exclude foo="bar">', "an attribute"),
            ("<Menu>\n  <Name>Applications</Name>", '<Menu foo="bar">\n  <Name>Applications</Name>',
             "an attribute on the root"),
            ("<Exclude>", "<Exclude>wsjtx.desktop", "stray text"),
            ("</Exclude>", "</Exclude>stray", "stray text after an element"),
            ("<Filename>station-manager-desktop.desktop</Filename>",
             "<Filename>station-manager-desktop.desktop<Filename>wsjtx.desktop</Filename></Filename>",
             "an element nested in a leaf"),
        ]:
            with self.subTest(why=why):
                m = valid()
                self.assertEqual(m["menu"].count(old), 1, old)
                m["menu"] = m["menu"].replace(old, new)
                self.assertReported(m, "menu: ")
        # The order of the two sections, and comments and whitespace, do not matter.
        m = valid()
        text = m["menu"]
        a, b = text.index("  <Menu>\n    <Name>Amateur Radio</Name>"), text.index("  <Menu>\n    <Name>Internet</Name>")
        end = text.rindex("</Menu>")
        m["menu"] = text[:a] + text[b:end] + "<!-- x -->\n" + text[a:b] + text[end:]
        self.assertEqual(problems(m), [])

    def test_menu_must_be_well_formed(self):
        for text in [None, "", "<Menu><Name>Applications</Name>", "not xml",
                     "<Other><Name>Applications</Name></Other>"]:
            m = valid()
            m["menu"] = text
            self.assertReported(m, "menu: ")

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
