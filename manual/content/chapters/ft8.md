---
title: Operating FT8
weight: 70
---

Station Manager allows you to easily make and log FT8 contacts without *any* other 3rd party software installed. The
user interface is easy to use and contacts can be forwarded to online services in real-time. Integration with
the [PSK reporter online service](https://pskreporter.info/) is also built-in.

## Getting started with FT8

### Enabling FT8

To use FT8 your transmitter must support CAT and be connected to the hardware where SM is installed and running.
See [Enabling CAT](#cat) for details.

### Before you transmit: set your rig's time-out timer

Do this once, before using any Station Manager feature that transmits (FT8 or
the amplifier Tune carrier): enable your rig's **TX time-out timer** (TOT) and
set it to a few minutes.

Station Manager takes transmit safety seriously — every transmission carries a
hard automatic stop, and the daemon drops the key on disconnects, errors, and
shutdown. But all of that protection travels over the same USB/serial cable as
everything else, and a cable, hub, or USB port **can fail while the rig is
keyed**. When that happens, no software on the computer can reach the radio to
unkey it — the "stop transmitting" command has no wire to travel down. The
rig's own time-out timer is the one safety net that lives inside the radio and
works when the cable doesn't: it unkeys automatically after the set time, no
matter what happened on the computer side.

Most rigs ship with the timer **off**. On the Yaesu FTdx10 it is under
**FUNC → OPERATION SETTING → GENERAL → TX TIME OUT TIMER**; other rigs index
it as "TOT" or "time-out timer" in the menu list. A setting of **3 minutes**
is a good choice: it will never interrupt an FT8 transmission (about 13
seconds) or the Tune carrier (30 seconds at most), leaves room for a long
voice over, and caps a fault at three minutes of carrier instead of an
unattended transmission that runs until you notice.

### Before you transmit: stop the computer sleeping

Also do this once, before transmitting: turn off **automatic suspend**
(sleep) on the computer running Station Manager.

> **Don't skip this one.** At best the results are unpredictable — a run that
> stops for no visible reason, or a rig that keys with no power output. At
> worst you can damage equipment. A transmitter left keyed with nobody at the
> radio runs until its time-out timer stops it, and if that timer is off — as
> it is on most rigs from the factory — until you come back and notice. A long
> unattended carrier is hard on a transmitter's finals, harder still on a
> linear amplifier driven far beyond its duty cycle, and will cook a balun,
> tuner, or feedline that was never specified for continuous key-down. It also
> leaves you transmitting unattended, which your licence may not permit.

Screen blanking and screen locking are harmless — Station Manager keeps
running, keeps decoding, and keeps transmitting perfectly well with the
screen dark. Sleep is different, because it stops the whole computer.

**Sleep is likely to catch you mid-transmission.** Calling CQ puts you on the
air for about 13 seconds in every 30, so an idle timer that expires during a
run has roughly two chances in five of landing while the rig is keyed. When
that happens the stop-transmit command is never sent — the program that would
send it is suspended along with everything else, and so is its own automatic
stop. The rig stays on the air until its time-out timer ends the
transmission. That is precisely the case the previous section exists for, and
the only protection left.

**Waking up resets the USB bus.** On resume the computer re-initialises its
USB ports, so your audio interface and your CAT connection are both
disconnected and re-detected. That normally recovers cleanly. When it doesn't,
CAT can come back while the audio path does not — and a rig being keyed with
no audio reaching it shows **TX with no power output**.

Turning it off:

- **KDE Plasma** — System Settings → Power Management → turn off *Suspend
  session*.
- **GNOME** — Settings → Power → *Automatic Suspend* → Off.
- **On a laptop**, also set the lid-close action to do nothing. Closing the
  lid suspends by default on most systems.
- **On a dedicated or headless station computer**, block it outright:

  ```
  sudo systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target
  ```

Leave the rig's time-out timer set regardless. It is the backstop for the
case where the computer cannot help you, and sleep is exactly that case.



- Enabling FT8 and what's required.
- Choosing your audio device.
- Band Activity: reading decodes, country flags, and beam headings.
- Answering a CQ: pick a clear transmit offset, click a CQ, and let the exchange auto-advance to 73.
- Calling CQ and working answerers.
- The Session tab and where your FT8 QSOs go.

## Operating FT8

### Answering a CQ

Answering a CQ is a common way to make an FT8 contact: another station calls CQ, you reply, and Station Manager runs
the rest of the exchange for you.

#### Before answering a CQ

Two things need to be in place first:

- **Enable transmit.** Click **Enable Tx** — it turns red to show transmit is armed.
  Until transmit is armed, Station Manager only listens, and clicking a CQ won't start
  a reply. Arming needs a connected, CAT-controlled rig; the button stays disabled
  otherwise.
- **Pick a transmit offset.** On the **Occupancy** tab the passband is laid out as a
  strip of frequency slots: busy slots are shaded, clear slots show as green markers,
  and a ★ marks Station Manager's top pick. Click a clear marker (or the **Clear
  Offsets** chip) to choose where your signal goes out, so you're not transmitting on
  top of another station.

Both settings stay put between contacts, so you normally do this once at the start of
a session.

### Making the contact

1. In **Band Activity**, find a station calling **CQ**. CQ lines stand out, and each
   carries a country flag and a beam heading — the short-path bearing from your grid to
   theirs — so you can turn the antenna before you reply.
2. **Click the CQ line.** Station Manager answers that station on your chosen offset, in
   the correct slot, and the exchange begins. If you click partway through a slot it can
   still reply in that same slot when there's time, so you don't lose a whole cycle.
3. From there it runs itself. The **Operate** tab shows the message ladder — your
   transmissions interleaved with the other station's — with the current step
   highlighted. The standard exchange advances automatically through the signal reports
   to the closing **RR73 / 73**.
4. When the exchange completes, the contact is **logged** and appears in the **Session**
   tab (shared with your Phone/CW session log, ready for email-out).

### Stopping early

Click **Abandon** at any point to stop the exchange — for instance if the other station
vanishes or starts working someone else. Abandon drops the contact immediately and
returns you to listening, still armed and ready for the next one.

> **Your reply may not go out the instant you click.** FT8 only transmits on slot
> boundaries, so there can be a short wait before your signal is sent. The **ON AIR**
> indicator lights while you are actually transmitting.

## Calling CQ: choosing your slot

FT8 runs on a strict 15-second grid, and every station transmits in one of two
alternating slots — commonly called the two *parities*. Counting from the top of
each minute (UTC):

- **Even** slots start at **:00** and **:30**.
- **Odd** slots start at **:15** and **:45**.

You transmit in one parity and listen in the other. The **CQ slot** selector next
to the **Call CQ** button decides which slot your CQ goes out in:

- **Next** *(default)* — call on the very next slot boundary, whatever its parity.
  This is the fastest start: your first CQ goes out within about 15 seconds.
- **Even** — call only in even slots (:00 / :30).
- **Odd** — call only in odd slots (:15 / :45).

When you pick **Even** or **Odd**, Station Manager waits for the next slot of that
parity before keying. If the very next boundary happens to be the *other* parity,
your first CQ is held back by one extra slot — so choosing a parity can add up to
roughly 15–30 seconds before transmission begins, compared with **Next**. This is
expected: it is the price of controlling which half of the cycle you call in.

Why pick a parity at all? To settle on a clear half of the cycle, to stay
consistent with how you've been operating, or to avoid a parity that's congested
where you are. If you just want to get on the air quickly, leave it on **Next**.

> **Note:** when you click **Call CQ** with **Even** or **Odd** selected, the
> button changes to *Calling CQ…* straight away, but the radio stays silent until
> the next slot of the chosen parity arrives. A short quiet gap before the first
> transmission is normal — Station Manager is waiting for your slot, not stalling.

## ARRL Field Day

Station Manager can make **ARRL Field Day** contacts over FT8 in both directions —
answering another station's `CQ FD`, and working stations that call **you**. (It does
not *call* `CQ FD` itself.) Field Day uses a different exchange from an ordinary
contact: instead of a signal report, each station sends its **class** (number of
transmitters plus a category letter, e.g. `2A`) and its **ARRL/RAC section** (e.g.
`EMA`, or `DX` if you are outside the US and Canada).

### Set your Field Day identity

Before operating Field Day you must tell Station Manager your own class and section.
These live in the configuration file (`config.json`) under `ft8.field_day` — stop the
daemon, edit, and restart:

```json
"ft8": {
  "field_day": {
    "class": "1D",
    "section": "DX",
    "default_rst_rcvd": "59"
  }
}
```

`config.json` is plain JSON and does not accept `//` comments — type the block as
shown. The fields are:

- **`class`** — your entry, `<transmitters><category>`, e.g. `2A`, `5F`.
- **`section`** — your ARRL/RAC section, or `DX` if you are outside the US and Canada.
- **`default_rst_rcvd`** — the signal report to record when none is exchanged; see
  "Signal reports" below.

If your class or section is not set, Station Manager will refuse to start a Field Day
contact (and tell you why) rather than send an incomplete exchange.

### Operating

- **Answering a `CQ FD`** (search & pounce): in **Band Activity**, a Field Day CQ shows
  as `CQ FD <call> <grid>`. Click it just like an ordinary CQ — Station Manager sends
  your class and section, reads theirs, and logs the contact.
- **Working a station that calls you**: when you are the sought-after station, callers
  reach you with their exchange — `<your-call> <their-call> <class> <section>` (for
  example `7Q5MLV K7IOC 1D WWA`). That line is clickable; clicking it works them with
  the Field Day exchange. This is usually your busiest path.

A logged Field Day contact records the other station's **class** and **section** and is
marked as an `ARRL-FD` contest QSO.

### Signal reports on Field Day

Field Day does not exchange a signal report on the air — the class and section take its
place. Station Manager still fills in the report fields so your log is complete:

- **RST sent** is set to **your measured signal-to-noise of the other station** (the
  same value you would have sent in an ordinary FT8 contact).
- **RST received** is set to the **default you configured** in `default_rst_rcvd`,
  because the other station never sends you one.

Why bother? Some **OQRS** (Online QSL Request Service) systems and log checkers require
both *RST sent* and *RST received* to be present, and will reject or flag a contact
with either left blank. Set `default_rst_rcvd` to whatever value those services expect
(commonly `59` or `599`). Leave it empty if you don't need it — then *RST received* is
simply left blank on Field Day contacts.
