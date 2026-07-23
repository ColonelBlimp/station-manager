---
title: Connecting Your Rig (CAT)
weight: 50
---

## Important: keep data-mode PTT off the control lines

Before you connect a rig for the first time, check **how your rig keys the
transmitter in data modes**. Many rigs can be set to key from one of the serial
control lines — **RTS** or **DTR** — on the very same USB/CAT port Station
Manager uses to read your frequency and mode.

**If your rig is set that way, any program holding that port open can hold your
transmitter keyed.** The rig is doing exactly what you told it to: the line is
asserted, so PTT is down. No CAT stop command can release it, because the
transmitter is not being keyed by CAT — it is being keyed by a wire. The rig
will report that it is "transmitting by other means", and it will keep
transmitting for as long as the line stays asserted.

**What to set:**

| Rig | Setting | Use |
|---|---|---|
| Yaesu (FTdx10, FT-710) | `RADIO SETTING → MODE PSK/DATA → RPTT SELECT` | **DAKY** (not RTS or DTR) |
| Yaesu (FTdx10, FT-710) | `RADIO SETTING → GENERAL → CAT RTS` | **DISABLE** |
| Icom (IC-7300) | `SET → Connectors → USB SEND` | **OFF** |

Station Manager opens every supported rig's port with **RTS and DTR
de-asserted**, so it will not key a rig configured this way. That is the
default for all shipped rigs and you should not need to change it.

> **Yaesu owners: you must also set `CAT RTS` to DISABLE.** On these rigs RTS
> does two unrelated jobs, and the menu decides which: `RPTT SELECT` can make
> it a data-mode PTT line, and `CAT RTS` can make it a CAT flow-control line.
> With `CAT RTS` set to ENABLE — Yaesu's factory default — the radio will not
> send CAT replies unless the computer asserts RTS. Because Station Manager
> keeps RTS de-asserted for safety, **CAT appears to connect and then nothing
> happens**: the app shows no frequency, no mode, and FT8 reports the rig as
> not live. There is no error message, because from the daemon's side the port
> opened normally and the commands went out — the rig simply never answers.
> Set `CAT RTS` to **DISABLE** and CAT starts working immediately; no restart
> is needed.

If you have a genuine reason to assert a line, you can override it per rig in
`config.json`:

```jsonc
"rigs": [
  {
    "id": 1,
    "model": "yaesu-ftdx10",
    "overrides": { "rts": true }   // only if you know your rig doesn't key on it
  }
]
```

Be careful with that override. Asserting a line on a rig that uses it as a PTT
source will transmit continuously for as long as the daemon is connected.

> **Older builds asserted these lines.** Versions before 2026-07-23 opened
> Yaesu ports with RTS and DTR **asserted**. On a station with
> `RPTT SELECT = RTS` this held the data-mode PTT down for the whole session
> and caused a tune carrier that would not stop. If you run an older build, set
> `RPTT SELECT` to **DAKY** on the rig.

---

*Draft outline — remaining content to be written.*

- What CAT gives you: live frequency, mode, and rig control from the browser.
- Choosing your rig and serial port.
- Adding yourself to the serial device group (find the real group — don't assume `dialout`).
- Rig-control shortcuts (band, VFO, frequency step).
- Supported rigs.
