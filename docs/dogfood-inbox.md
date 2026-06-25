# Dogfooding inbox — raw capture

Quick, unsorted notes jotted while operating, via the `/log` command. **Raw
capture only** — items here are NOT triaged, deduped, or acted on. They may be
bugs, enhancements, half-thoughts, or non-issues.

**Triage later:** in a dedicated pass we go through these together and move each
to its real home — a fix, a `docs/backlog.md` entry, or struck through / deleted
as a non-issue. Don't let this file become the backlog; it's the in-tray.

Format: one bullet per note, newest at the bottom, date-stamped `[YYYY-MM-DD]`.

## Notes

<!-- /log appends below this line -->
- ~~[2026-06-23] default_rig_id is 0 on fresh install, Rig shows not set~~ **RESOLVED/WAI 2026-06-25** — daemon migrates a loose bridge/cat config into a one-entry catalogue + sets default_rig_id; a genuinely rig-less config correctly shows "not set"; commit 8c2fa625 makes the first rig added in the config SPA active. No bug remaining.
- ~~[2026-06-24] browser tab icon is the default. Should be the radio tower.~~ **FIXED 2026-06-25** — `assets/logo.png` (the tower mark) wired as the favicon for all three SPAs via each `public/logo.png`; Vite base-rewrites the href per SPA mount.
- [2026-06-24] we need some links in the logging SPA (and the other SPAs) to other URIs: config, logbook, db manager, etc
- [2026-06-24] we need to implement UI themes as well as dark mode
- [2026-06-24] the initial setup page should say something like, 'if you want to continue configuring Station Manager, click here [link]' which should go to the config SPA
- ~~[2026-06-24] all password/secret input field should be able to 'display' the password (the little eye glyph)~~ **FIXED 2026-06-25** — new reusable `PasswordField` (eye/eye-slash toggle) swapped into all config-SPA password inputs: Email (SMTP), Enrichment (QRZ), Forwarding (creds).
- ~~[2026-06-25] psk reporter config not seen~~ **FIXED 2026-06-25** — added a PSK Reporter section to the config SPA's FT8 tab (enable + host/port, daemon-validated), folded into the FT8 save with a restart-required banner.
- ~~[2026-06-25] qsl fields not seen~~ **FIXED 2026-06-25** — added a QSL defaults section (QSL_VIA / QSLMSG / QSL_SENT_VIA) to the config SPA's Station tab, folded into the Station save (the daemon already handled the `qsl` block presence-aware).
- ~~[2026-06-25] lat and lon are no calculated when the config SPA is saved, but it is updated in My Station LSPA~~ **FIXED 2026-06-25** — root cause was no gridsquare field in the CSPA; the daemon already re-derives lat/lon from grid on every PUT.
- ~~[2026-06-25] lat and lon needs gridsquare which is not edited in the config spa~~ **FIXED 2026-06-25** — added Station identity section (callsign/operator/owner/name/grid) to the CSPA Station tab.
- ~~[2026-06-25] operator name cannot be edited via CSPA~~ **FIXED 2026-06-25** — same Station identity section; identity now editable in BOTH SPAs (one daemon source of truth, ADR 0003).
- ~~[2026-06-25] mode-mappings should be removed from the LSPA~~ **FIXED 2026-06-25** — removed the Mode Mappings sub-tab (+ its `editingModes`/`snapModeMappings`/`$effect.pre` snap machinery and the mode-mapping PUT payload) from the logging SPA's My Station panel; mode-mapping editing now lives only in the config SPA (Rigs tab → `ModeMappingsEditor`).
- ~~[2026-06-25] CW setting should be removed from the LSPA~~ **FIXED 2026-06-25** — removed the CW sub-tab (Morse Key Type/Info) from the logging SPA's My Station panel; those fields now live only in the config SPA's Station tab. The logging SPA still sends `my_morse_key_*` unchanged on PUT (the daemon full-replaces `logging_station`), so the values aren't cleared.
- [2026-06-25] install/setup docs: rig should be on BEFORE running the config SPA otherwise rig's serial port may not show up in the list
