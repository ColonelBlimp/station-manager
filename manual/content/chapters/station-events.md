---
title: Station Events
weight: 105
---

**Station Events** is the page for what happened while you were not looking. It sits in
the sidebar between **Logbook** and **Settings** and lists, newest first, the events Station
Manager keeps on purpose: the alarms it raised and cleared, a transmit disarm it did on its
own, an FT8 exchange it had to end, and the two failures it has always kept — an ADIF export
that failed and an upload that finally gave up. Toasts vanish after a few seconds; these rows
stay.

### What is recorded

Every row names its kind, says when it happened, and carries the build of Station Manager that
recorded it.

- **TX alarm raised / TX alarm cleared.** The rig did not confirm it had stopped transmitting
  (see [Troubleshooting](#troubleshooting), *The rig transmits and won't stop*). The cleared row
  says how long the alarm stood — a transient alarm that raised and cleared within a second
  while you were away from the desk shows as exactly that.
- **Drive alarm raised / Drive alarm recovered.** The rig was keyed but the meter reported no
  output, and later a healthy transmission proved the output was back.
- **TX disarmed automatically.** Transmit was disarmed without you asking — the CAT connection
  was lost, or the rig left the frequency the session was bound to — while no contact was in
  progress.
- **Exchange ended by the daemon.** An FT8 contact in progress was ended by Station Manager,
  not by you: the FT8 view's event connection ended, CAT was lost, the rig moved or its
  frequency became unreadable, transmit was no longer armed, or the message could not be
  encoded. The row names the other station and the step the exchange was on.
- **ADIF export failed** and **Upload failed** — the two notification kinds, as before.

### What is deliberately not recorded

Routine activity has its own home and is not repeated here: contacts logged (the Logbook),
successful uploads (their own surfaces), FT8 sessions starting and stopping, decodes. Your own
actions are not events either: stopping or abandoning a contact, changing band, disarming
transmit yourself, or shutting the daemon down record nothing. An automatic disarm after the
last FT8 view disconnects records nothing when there is no partner exchange, including a
Call-CQ run waiting between contacts. If it ends an exchange in progress, one termination
is recorded.

Dismissing an alarm banner is not an acknowledgement, and this page does not mark anything as
read. It is a record, not an inbox.

### Reading the page

Two rows of chips narrow the list: by category (**Notifications** or **Alarms**) and by
severity (**Info**, **Warn**, **Error**). Each choice asks the daemon again, and the line above
the list says what is shown — *2 alarm events at severity error* — so an empty list under a
filter reads as a filter, not a fault. **Refresh** re-reads; the page does not update itself
while open.

A row reading *Details unavailable* means Station Manager did not recognise the stored detail
for that kind — usually a page older or newer than the daemon. Nothing raw is ever shown in its
place.

Station Manager keeps the newest 500 rows of each category and drops the oldest beyond that.
The rows live in the station database, so they survive a restart and a browser reload; the
daemon's own log file, `smd.log`, remains the full diagnostic record and is not mirrored here.
