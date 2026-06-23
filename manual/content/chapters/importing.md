---
title: Importing an Existing Log
weight: 90
---

*Draft outline — content to be written.*

- Importing an ADIF file with `smctl import` (wraps the stop/import/restart
  database hand-off so you run one command).
- **Uploads default to OFF:** import seeds your local logbook only and never
  re-sends your history to QRZ/ClubLog. Opt in per-forwarder with `--forward
  <name>` only when you actually want the imported log pushed to a service.
- Bulk-forwarding an already-imported log to a newly-subscribed service is a
  one-click action in the logbook app — not a re-import.
- The QRZ per-QSO logid (`app_qrzlog_logid`) is preserved as provenance on each
  imported QSO, independent of any upload.
- Setting the station callsign for the imported log.
