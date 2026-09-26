---
title: QSO Archives
weight: 45
---

An **archive** is one QSO database file with its own logbooks. Your
station starts with one archive, **Home**: the database you have always
logged into, adopted in place. You can create more — a contest, a
DXpedition, a club station — and switch between them. One archive is
active at a time, and everything you log goes into it.

## Where to find them

**Settings → Archives** lists every archive with its state:

- **Active** — the archive Station Manager is logging into now.
- **Pending restart** — you asked to switch to it; the daemon is restarting
  to open it.
- **Inactive** — available, not open.

The header also shows the active archive above the logbook name. Picking
another archive there does the same as **Activate** in Settings.

## Creating an archive

Give it a label, a name for its first logbook and the callsign that logbook
logs under, then **Create archive**. The file is created under Station
Manager's data directory; you never choose a path. The new archive is
inactive until you activate it.

A new archive uploads nowhere: every destination starts off, SM Cloud
included. Once it is active, turn on the ones it should use on **Settings →
Forwarding** (see the Forwarding chapter).

## Switching archives

**Activate** restarts the daemon — that is how the switch happens; the
running daemon never swaps databases underneath you. Before you confirm:

- stop any transmission and **disarm FT8**. A switch is refused while FT8
  is armed, a message is in flight, a contact is in progress, or the tune
  carrier is keyed, and it says which.
- expect the connection to drop for about five seconds, after which the
  page reloads on its own so everything — the logbook name and count, the
  contact form, the log view — belongs to the new archive. Unsaved Settings
  edits are lost, so save first.

Once the daemon is back and the page has reloaded, the list and the header
show which archive is active. If Station Manager cannot tell whether the
daemon has restarted, the page is covered by an **Archive binding
unproven** notice and logging and transmitting are paused, so nothing can
land in the wrong archive; press **Reload now** (the page also reloads by
itself once it can see the daemon again). The same notice appears if the
page loads while the daemon is unreachable; it clears itself with a reload
as soon as the daemon answers. Any other Station Manager
tab you have open — another Operate view, the Logbook, a map on a second
screen — notices the change when it reconnects and reloads itself too. If the new archive could not be opened, the previous one stays
active and the archive you chose shows a ⚠ after its label. Point at it to
read **Last activation failed** with the reason.

While a switch is pending, transmit is sealed: arming FT8, sending, starting
a contact or keying the tune carrier is refused with *archive switch pending*
until the restart completes.

## What stays shared

Callsign lookups, the enrichment cache and the FT8 evidence file are
station-wide and do not change with the archive, and so are the station
accounts on **Settings → Forwarding**: SM Cloud's service and token.

Where each archive uploads is its own. Each archive keeps its destinations,
logbook by logbook, with each logbook's own account for each service; the
Forwarding tab edits those of the active archive only. SM Cloud is the one
exception for now: it can be turned on only in the Home archive, and its
card says so in any other.
