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

Indented below each archive are all of its logbooks: the logbook name, its
callsign and its QSO count. The list never hides extra logbooks. **Default**
marks the logbook the archive logs to while it is active — on the active
archive, the logbook your QSOs go to now. **1 QSO** is
singular; larger counts include their thousands separator and **QSOs**.

The active archive's counts are kept current as you log and edit. For an
inactive archive, Station Manager remembers the contents from its last clean
close, creation, import or restore. If the file has changed since that summary,
the list keeps the last-known logbooks and says **Counts may be out of date —
the file changed since it was last open**. **Not known until it has been
opened** means no summary exists yet; **No logbooks** means the archive is known
to be empty. The active archive is open, so it says **Counts are being
updated** while a change is recounted, and **Current counts are not available**
if its counts could not be read.

The header also shows the active archive above the logbook name. Picking
another archive there does the same as **Activate** in Settings.

## Logbooks

**Settings → Logbooks** manages the logbooks of the **active** archive — the
one Station Manager has open. To add a logbook to another archive, activate
that archive first.

- **Add logbook** takes a name and a callsign. The callsign starts as your
  station callsign; any valid callsign is accepted. Adding a logbook does not
  switch archives, does not make it the Default, and turns on no uploads you
  did not choose.
- **Live contacts keep going to the Default logbook.** A new logbook receives
  QSOs by import or restore (see [Importing](#importing)). A logbook whose
  callsign differs from your station callsign cannot receive live contacts
  yet.
- **Upload to SM Cloud** appears, unticked, only when this archive can upload
  to SM Cloud. Ticked, the new logbook's SM Cloud uploads start after a
  restart. Other services are set up per logbook on the **Forwarding** tab.
- **Rename** changes the name; a logbook's callsign cannot be changed.
- **Delete** works only on an empty logbook that is not the Default; the
  button's tooltip says why when it is unavailable.

## Creating an archive

Give it a label, a name for its first logbook and the callsign that logbook
logs under, then **Create archive**. The file is created under Station
Manager's data directory; you never choose a path. The new archive is
inactive until you activate it.

A new archive uploads nowhere: every destination starts off, SM Cloud
included. Once it is active, turn on the ones it should use on **Settings →
Forwarding** (see the Forwarding chapter).

SM Cloud is the exception for now: it can be turned on only in **Home**, the
original archive. In an archive you created, its QSOs stay local, and SM Cloud's
destination card on **Settings → Forwarding** says so. A later release lifts
this.

## Switching archives

**Activate** restarts the daemon — that is how the switch happens; the
running daemon never swaps databases underneath you. Before you confirm:

- stop any transmission and **disarm FT8**. A switch is refused while FT8
  is armed, a message is in flight, a contact is in progress, or the tune
  carrier is keyed, and it says which.
- log or clear the QSO you are typing on **Phone / CW**. A switch is refused
  while this page holds one, even a partly typed one, and the Phone / CW form
  is locked while the switch runs so nothing new is typed into a page about
  to reload.
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

If a tab that reloads this way holds an unlogged Phone / CW QSO, it saves it in this
browser before it reloads, together with the archive and logbook it belongs
to and the rig frequency, band and mode it had. The QSO is **not logged**: it is
kept only in this browser until you restore and log it or discard it. After the
reload a message says so once, with an **Open Unlogged QSOs** button, and has no
automatic timeout (like any message, it can still be pushed out when several
arrive at once). **Unlogged QSOs (N)** appears in the header beside Logbook (in
the map tab, in its toolbar). It stays there on every page while any kept QSO
remains, in this tab and any other Station Manager tab open in this browser.
Open it to see each QSO: **Show details** shows every value so
you can read or **Copy** it, and **Discard** removes the saved copy (only the
copy in this browser — nothing in any logbook changes). Closing the panel, or
pressing Esc, keeps every saved QSO and never clears the QSO you are typing. A QSO whose Log
attempt had an unknown outcome says so: check the Logbook in its archive
before logging it again. Unlogged QSOs are never logged automatically.

To log one, open **Unlogged QSOs** on Phone / CW while the archive and logbook
it came from are in use, and press **Restore**. The QSO returns to the form with
its original times and the rig values it had. Confirm or correct those values,
then log it as usual: it goes into its original logbook with the operator
details it was saved with, and the form is read-only while it is being logged.
Only one tab can hold a restored QSO at a time. A QSO from another archive or
logbook says which one it belongs to and offers no Restore here. On other
pages, **Go to Phone / CW** takes you there without restoring anything.

If the
browser cannot save the QSO, that tab does not reload: it shows the QSO in
full, with **Retry save** and **Discard and reload**, and stop controls stay
available.

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
