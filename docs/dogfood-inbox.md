# Dogfood inbox

Raw dogfooding notes for later triage.

## Notes
- [2026-09-05] alpha.2 pass-1 B1-03 (Settings → Rigs): the default rig profile shows an ENABLED Delete button. Operator: the default rig must not be deletable; require changing the default first. Not pressed. Check whether the daemon refuses server-side or only the UI affordance is wrong.
- [2026-09-05] alpha.2 pass-1 B1-03: the manual link in the app opens in the same tab; operator expects it to open in a new tab.
- [2026-09-05] alpha.2 pass-1 follow-up (operator decision): change SM Cloud (smcloud) to use https even on the LAN, instead of relying on the allow_insecure_http cleartext acknowledgement that Finding 1 forced onto the live config.
- [2026-09-05] alpha.2 pass-2 start 20:34:48 CAT: bridge logged "TX ALARM — the rig may still be transmitting; check the radio" (error) three seconds after the FTdx10 CAT port opened, then "tx alarm cleared (transmitter confirmed idle)" in the same second; operator confirms nothing was transmitting. Not seen at the 19:49 or 20:10 starts. Startup transient / false TX alarm at bridge open — investigate the first TX-state read after port open.
- [2026-09-05] alpha.2 live FT8 receive (evening 2026-09-05): decodes show callsigns in angle brackets, e.g. <VU24DX> and EY35D — valid special-event/nonstandard calls rendered with <..>. Operator noticed; a nonstandard station has appeared on air, the trigger the operator named for W-0002.
