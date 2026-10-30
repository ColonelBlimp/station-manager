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

The header also shows the active archive above the logbook name. It only
names it: archives are switched from **Settings → Archives** alone.

## Logbooks

**Settings → Logbooks** manages the logbooks of the **active** archive — the
one Station Manager has open. To add a logbook to another archive, activate
that archive first.

- **Add logbook** takes a name, a callsign and an optional description,
  which the list shows under the name. The callsign starts as your station
  callsign; any valid callsign is accepted. Adding a logbook does not
  switch archives, does not make it the Default, and turns on no uploads you
  did not choose.
- **Live contacts — Phone / CW and FT8 — go to the Default logbook.** Another
  logbook receives QSOs by import or restore (see [Importing](#importing)),
  or becomes the Default with **Set as default** (below).
- **Set as default** appears on each logbook other than the Default. It can
  be used on a logbook whose callsign matches the Default's; on one with
  another callsign it is unavailable, and its tooltip says why — such a
  logbook cannot become the Default.
- **Upload to SM Cloud** appears, unticked, only when a new logbook in this
  archive can upload to SM Cloud: the station has an SM Cloud account, and
  the server's support for keeping logbooks apart has been checked (see
  Forwarding). Ticked, the new logbook's SM Cloud uploads start after a
  restart, under its own identity. Other services are set up per logbook on
  the **Forwarding** tab.
- **Edit** changes the name and the description; clearing the description
  removes it. A logbook's callsign cannot be changed.
- **Delete** works only on an empty logbook that is not the Default, and not
  on a logbook a default change still names; the button's tooltip says why
  when it is unavailable.

### Changing the Default logbook

**Set as default** restarts Station Manager, as switching archives does: the
new Default takes effect when the daemon starts again, and every open window
reloads. Before you confirm:

- stop any transmission and **disarm FT8**. The change is refused while FT8
  is armed or running, or the tune carrier is keyed, and it says which. It is
  also refused while a QSO is still being logged in this window, while an
  archive switch or a Settings save is in progress, and while another default
  change is unresolved.
- read the confirmation. It names the logbook live contacts will go to after
  the restart. If this window holds a QSO you are typing on Phone / CW, even a
  partly typed one, it is named: **OK** discards it and restarts, **Cancel**
  keeps it and changes nothing. If a Log's outcome is unknown, the
  confirmation says the QSO **may already be logged**: check the current
  Default's Logbook before logging it again. Unsaved Settings edits in this
  window are named too: the reload loses them.
- **every other open window loses its unlogged work** — a QSO typed but not
  logged and unsaved Settings changes — exactly as for an archive switch.
  Logged QSOs are never affected.
- if the current Default uploads to SM Cloud and its binding has not yet been
  adopted, reserved or created on its own identity, the confirmation says so:
  its existing uploads continue, but to start its adoption later you make it
  the Default again. A binding that is already adopted, reserved or created
  on its identity is not affected, and no note is shown.

From the request until the restart, logging and transmitting in this window
are paused, and a banner at the top of the main app views says so (the
standalone Map view has none). The page reloads
on its own when the daemon is back.

If the change does not complete normally, **Settings → Logbooks** shows what
happened above the list, and the banner links there:

- *applies at the next restart*, or *not applied at this start and still
  pending* with the reason: **Cancel change** withdraws it.
- *not yet confirmed on disk*: the change was cancelled, withdrawn or
  abandoned, but if power is lost before that is confirmed, the change may
  still apply at a later start. **Confirm** writes it again.
- *Cleanup pending*: the change applied, but its record has not been removed
  durably; **Retry cleanup** removes it (the next start also does). Set as
  default and archive switching are unavailable until it is.
- a change that was cancelled, withdrawn or not applied is reported with its
  reason, and needs nothing from you.

Each action names the change you were shown and is sent once. If its answer
is lost, Station Manager reads the state again rather than repeating it.

If Station Manager cannot read the state of a default change while logging
is paused for it — when the page loads, after a restart it could not
confirm, or while a change is pending — logging and transmitting stay
paused, and the banner offers **Check again** and **Reload**. Once logging
has been confirmed open, a later failed read does not pause it again.

## Creating an archive

Give it a label, a name for its first logbook and the callsign that logbook
logs under, then **Create archive**. The file is created under Station
Manager's data directory; you never choose a path. The new archive is
inactive until you activate it.

A new archive uploads nowhere: every destination starts off, SM Cloud
included. Once it is active, turn on the ones it should use on **Settings →
Forwarding** (see the Forwarding chapter).

SM Cloud can be turned on in an archive you created as in Home, once the
server's support has been checked. Each logbook is backed up under its own
identity, so two archives with the same label and logbook names never mix
their QSOs. The first turn-on backs up the whole logbook (see Forwarding).

## Switching archives

**Activate** restarts the daemon — that is how the switch happens; the
running daemon never swaps databases underneath you. Before you confirm:

- stop any transmission and **disarm FT8**. A switch is refused while FT8
  is armed, a message is in flight, a contact is in progress, or the tune
  carrier is keyed, and it says which.
- log or clear the QSO you are typing on **Phone / CW**. If this window holds
  one, even a partly typed one, the confirmation names it: **OK** discards it
  and switches, **Cancel** keeps it and switches nothing. A switch is refused
  while a QSO is still being logged; wait for the Log to finish. If a Log's
  outcome is unknown, the confirmation says the QSO **may already be logged**:
  check the Logbook in the current archive before logging it again. The
  Phone / CW form is locked while the switch runs so nothing new is typed into
  a page about to reload. Opening Settings never clears the form.
- expect the connection to drop for about five seconds, after which the
  page reloads on its own so everything — the logbook name and count, the
  contact form, the log view — belongs to the new archive. Unsaved Settings
  edits are lost, so save first.

Each archive keeps its own **Session** list in a browser tab. After a switch
the Session panel, its count and its edit and export show only the contacts
logged in that archive during this sitting, and an archive you have not used
in this tab starts with an empty list even if its logbook has contacts.
Switch back and that archive's list returns. Your logged QSOs are never moved
or deleted by a switch; the Session list is only this tab's view of them.
While the page is still confirming which archive the daemon serves, Session
edit, export and email wait until it has. The first time a tab loads after
the update that introduced this, its old Session list is cleared and a notice
says so; your logged QSOs are intact in the Logbook.

An export or email from the Session list reports what the daemon confirmed
for the rows selected when you clicked. If anything is short, the notice
names every part, for example "Exported 3 of 5 selected QSOs. 1 could not be
found in this archive. 1 has no QSO ID." Rows that could not be found stay
in the list, and the next export says so again. If the daemon's answer does
not account for every row, the file is still saved but you are asked to check
it, and an email marks nothing as emailed.

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

**Every other open window loses its unlogged work.** Switching restarts
Station Manager and reloads every open window. In any window other than the
one you switched from, unlogged work is lost: a QSO typed on Phone / CW but
not logged, an FT8 / FT4 exchange not yet completed, and unsaved Settings
changes. Nothing warns that window; the confirmation is the warning. Logged
QSOs are never affected. Before switching, log or clear your work in every
window.

While a switch is pending, transmit is sealed: arming FT8, sending, starting
a contact or keying the tune carrier is refused with *archive switch pending*
until the restart completes.

## What stays shared

Callsign lookups, the enrichment cache and the FT8 evidence file are
station-wide and do not change with the archive, and so are the station
accounts on **Settings → Forwarding**: SM Cloud's service and token.

Where each archive uploads is its own. Each archive keeps its destinations,
logbook by logbook, with each logbook's own account for each service; the
Forwarding tab edits those of the active archive only. SM Cloud backs each
logbook up under its own identity, so archives never share an SM Cloud backup,
even when their labels and logbook names match (see Forwarding).
