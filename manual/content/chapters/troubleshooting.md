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

## The rig transmits and won't stop — "CHECK YOUR RADIO"

**Act first, diagnose after.** Unkey at the radio, or switch it off. Your rig's
**TX time-out timer (TOT)** is the backstop that ends a stuck transmission when
nothing else can — set it, and treat it as a requirement rather than an option
if you use tune or FT8. Station Manager will refuse to start any new
transmission while it is unsure, so it cannot make things worse while you sort
the radio out.

**The most common cause is a control line keying the rig.** If your rig keys
data-mode PTT from **RTS** or **DTR** on the CAT port, the transmitter is held
down by a wire, and no stop command over CAT can release it. See
[Connecting Your Rig (CAT)](#cat) — set `RPTT SELECT` to
**DAKY** (Yaesu) or `USB SEND` to **OFF** (Icom).

**Another cause is the computer going to sleep.** If an idle timer suspends
the machine part-way through a transmission, Station Manager is frozen with
the rig keyed — it cannot send the stop command, and neither can anything else
on the computer. Only the rig's time-out timer will end it. Turn automatic
suspend off: see [Operating FT8](#ft8), *Before you transmit: stop the
computer sleeping*.

**How to tell.** In `smd.log`, look at what the rig answered after the stop:

```
grep 'tx-status' ~/.local/share/station-manager/log/smd.log | tail
```

- `status 0` — the rig is in receive. Normal.
- `status 2` — "transmitting by **other means**". Expect this briefly while a
  transmission is actually up. Seeing it **at rest**, when you are not
  transmitting, means something other than CAT is keying your rig — almost
  always a control line, as above.
- `rig reports CAT TX still keyed after unkey` — the rig acknowledged that it
  ignored the stop command. Check the radio.

Station Manager logs `rig tx-status changed` on every change of state, whether
or not it was expecting one, so a line that keys the rig behind its back shows
up in the log even when nothing else looks wrong.

**The banner won't clear.** It clears when the rig reports that it is receiving
again — the daemon re-asks automatically for a few minutes, and there is a
**Re-check** button on the banner to ask again at any time. Dismiss only hides
the banner; it does not claim the rig is safe. If the rig genuinely cannot be
reached, restarting Station Manager re-establishes the connection and resets
the state.

---

## CAT connects but no frequency ever appears (Yaesu)

The app shows no frequency and no mode, FT8 reports the rig as not live, and
there is **no error** anywhere. The log says the port opened and the pipeline
started, then goes quiet.

On a Yaesu, check `OPERATION SETTING → GENERAL → CAT RTS` and set it to
**DISABLE**. With it set to ENABLE — the factory default — the radio only
answers CAT while the computer asserts the RTS line, and Station Manager keeps
RTS de-asserted so it can never key your transmitter by accident. The commands
reach the rig; the replies never come back. Nothing reports an error because,
from the software's side, nothing failed.

This is the one rig setting that has to change before CAT will work at all.
See [Connecting Your Rig (CAT)](#cat) for the full picture, including why RTS
is left de-asserted.

---

*Draft outline — remaining content to be written.*

- Station Manager won't start — and where the startup error is logged.
- The rig won't connect.
- FT8 says it's idle.
- Where the logs are.
- Reading this manual when the daemon is down (open it from disk).
