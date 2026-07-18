---
title: Troubleshooting
weight: 110
---

## A logged QSO has no operator name (or other looked-up details)

**Why it happens.** When you log a QSO, Station Manager looks the callsign up
online (QRZ.com and the country service) to fill in the operator's name,
country, DXCC entity, zones, and grid. By design, **a failed lookup never
stops the QSO from being logged** — if the internet is slow or down at that
moment, the contact is stored with whatever was known, and fields like the
name are simply left blank. The QSO itself is safe; only the decoration is
missing.

**The fix — Re-enrich.** Open the **Logbook**, click the QSO to open its
edit window, and press **Re-enrich** (bottom-left). Station Manager re-runs
the lookup and fills in the missing fields; check the result and **Save**.
To repair a batch, tick the affected rows in the logbook list and use the
bulk **Re-enrich** action instead — it sweeps every selected row on the
page.

**If the name still comes back blank**, the callsign is probably not listed
on QRZ.com at all (common for some regions and club calls). No lookup source
has the name in that case — Re-enrich will still repair the country, DXCC,
and zones, and you can type the name in by hand if the operator gave it to
you on the air.

---

*Draft outline — remaining content to be written.*

- Station Manager won't start — and where the startup error is logged.
- The rig won't connect.
- FT8 says it's idle.
- Where the logs are.
- Reading this manual when the daemon is down (open it from disk).
