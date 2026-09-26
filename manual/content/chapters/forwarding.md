---
title: Forwarding Your Log (QRZ, Club Log, QRZCQ, and SM Cloud)
weight: 80
---

Forwarding sends each QSO you log to your online services automatically.
When you log a contact, Station Manager records it locally first — that
never depends on the network — and then, in the background, uploads it to
the destinations its logbook is bound to. If your internet is slow or drops
out, the upload waits and retries; your logging is never blocked.

Four destinations are supported today: **QRZ.com**, **Club Log**,
**QRZCQ**, and **SM Cloud backup** (for an SM Cloud service you run).
LoTW is planned but not yet available — see the end of this chapter.

### Where forwarding is set up

Everything is on **Settings → Forwarding**, in two parts:

- **Destinations for Home** (named after the active archive) — which services it
  uploads to, logbook by logbook, and each logbook's own account for each
  service: a QRZ logbook key, a Club Log login. These belong to the archive
  (see the QSO Archives chapter): activating another archive brings its own
  destinations, and a new archive starts with every destination off.
- **Station accounts** — what every archive shares: the SM Cloud service
  address and token.

Station Manager never shows a stored password or key again. A field that
holds one reads **✓ saved**, with **Replace** to type a new value and, where
it can be removed, **Remove**. Replace opens an empty box; **Cancel** closes
it again and keeps the saved value. The same applies on the Email and
Enrichment tabs.

**Changes apply when the daemon restarts.** After you save, the tab shows
*Saved changes apply after a restart* with a **Restart daemon** button; press
it when you are ready. If a save is refused — a required field left blank,
say — a message says why and the field is marked.

### Station accounts

Station accounts are the settings every archive shares. They live in
`config.json`, not in an archive, so they stay the same whichever archive is
active. Today that is SM Cloud's **Service URL** and **Bearer token**. Press
the section's own **Save**; like destinations, changes apply when the daemon
restarts.

Club Log is not listed here. Its application key is built into Station
Manager, and its application password belongs to your Club Log account, so
you enter it per logbook under the destinations.

### Turning a destination on

1. Open the destination's card — each card shows whether it is **enabled**
   for every logbook, **disabled**, or **mixed**: on for some logbooks, with
   the ones it is off for named beside it.
2. Tick the switch for the logbook that should upload. With several
   logbooks in the archive, the destination's own switch sets them all at
   once.
3. Fill that logbook's account fields, described per service below. A
   logbook switched on without a required field is not saved: the field is
   marked *Required to turn this on*.
4. Press **Save destinations**, then restart the daemon.

If a destination can't be turned on in this archive, its card says why and
the switches stay off. When the missing piece is a station account, the note
offers **Open its station account**, which opens that card below.

### QRZ.com

QRZ needs a **logbook API key** per logbook. Each QRZ logbook has its own
key, found on the logbook's settings page on QRZ.com. That one key both
authenticates you and selects which QRZ logbook the QSO lands in, so two
Station Manager logbooks can upload to two different QRZ logbooks.

QRZ supports the full lifecycle, so if you later edit or delete a QSO in
Station Manager, that change is forwarded too.

### Club Log

Club Log needs **two separate credentials**, and this trips people up
because the names sound alike:

- **Your account login**, per logbook — **Account email** plus
  **Application password**. Use a Club Log Application Password (generate
  one in your Club Log account settings) rather than your main login
  password, so it can be revoked on its own. This tells Club Log *whose*
  log the QSO belongs to. **Callsign** selects which of your account's logs
  receives it. Leave it blank to use the logbook's own callsign: Station
  Manager fills it in when you save and keeps it, so changing the logbook's
  callsign later does not move your uploads to another Club Log log.
- **The application API key**, which identifies *Station Manager as a piece
  of software*, not you. It is built into Station Manager when it is
  compiled, so there is nothing to type. If your build lacks it, the Club
  Log destination card says so. Without it, Club Log uploads wait in the
  queue until a build with the key is installed; nothing is lost.

#### Club Log only uploads and deletes — it does not edit

Club Log's real-time interface can **add** a QSO and **delete** a QSO,
but it cannot change the fields of one already in your log (re-sending an
edited QSO is just treated as a duplicate). Station Manager knows this, so
editing a QSO won't pile up failed Club Log uploads.

#### If your credentials are wrong

If Club Log rejects your login, it requires software to **stop sending
immediately** — otherwise your address can be temporarily blocked.
Station Manager honours this: the first rejection halts further Club Log
uploads until you fix the credentials and restart the daemon. So if Club
Log uploads stop, check the logbook's Club Log fields on **Settings →
Forwarding**, correct them, save, and restart.

### QRZCQ

QRZCQ needs, per logbook, the **QRZCQ callsign** of your account and that
account's **API key**.

QRZCQ asks clients not to post more than once per minute. Station Manager is
deliberately gentler: it sends one QSO every **90 seconds**, and enforces
that minimum interval even if the pacing is changed by hand. A backlog
therefore drains gradually without holding up local logging.

The published QRZCQ developer API documents adding log records, but documents
no edit or delete operation. Station Manager does not invent those semantics,
so later edits and deletes remain local and are not sent to QRZCQ. QRZCQ
describes this authenticated JSON account API as alpha, so its wire format may
change upstream.

### SM Cloud backup

SM Cloud's **Service URL** and **Bearer token** are a station account: set
them once under **Station accounts**. Each logbook may name its **Cloud
logbook**; leave it empty for `main`. For now SM Cloud can be turned on only
in the Home archive, and its card says so in any other.

### How to tell a QSO was uploaded

After a successful QRZ.com or Club Log upload, Station Manager stamps the QSO
with the service's standard ADIF status field — `QRZCOM_QSO_UPLOAD_STATUS` for
QRZ or `CLUBLOG_QSO_UPLOAD_STATUS` for Club Log — set to `Y`, along with the
date. These stamps travel with the QSO in ADIF exports, so they survive
exporting and re-importing your log.

ADIF defines no equivalent QRZCQ upload-status field. A successful QRZCQ upload
is therefore recorded in Station Manager's durable upload history, but it does
not add a portable QRZCQ status tag to an ADIF export.

### Turning a destination off

Switching a logbook's destination off means Station Manager **stops
queuing** that logbook's new QSOs for that service once the daemon restarts.
It is *not* a pause-and-catch-up: that logbook's uploads still waiting for
the service are dropped from the queue at the restart (the contacts
themselves are untouched in your log — only the pending upload is cleared).
This is deliberate — it's the clean way to keep a batch of QSOs (say, a
contest) off a service. When you turn the destination back on, new QSOs
queue again, but the ones logged while it was off do **not** get sent
automatically. You send those yourself — see "Catching up" below.

### The queue: retrying and clearing

A logbook with uploads waiting, failed or in flight shows its queue under that
destination (an idle logbook shows none),
for example *12 waiting · 1 failed · 3 in flight*: **waiting** is the
backlog still to send, **failed** is uploads the service refused and that
won't be retried on their own, and **in flight** is the small batch the
uploader is sending right now.

- **Retry failed** puts that logbook's failed uploads back in the queue —
  useful once you have corrected the cause, such as a wrong key.
- **Clear queue** discards that logbook's waiting and failed uploads for
  that destination, after asking you to confirm. It does not touch your
  logged contacts, anything already uploaded, or the batch in flight. It
  reports how many uploads it removed.

For a service that stamps its uploads (QRZ.com, Club Log), a logbook with
failed uploads also offers **Show the QSOs of … not on …**. It opens that
logbook in the Logbook view, listing every QSO not yet on the service — not
only the failed ones — ready for "Catching up" below.

### Catching up: sending past QSOs to a service (backfill)

QSOs logged while a service was off, logged before you added it, or
imported from another log won't have been uploaded to that service. You
can send them yourself from the Logbook view:

1. Pick the destination (e.g. QRZ) and the view shows you the contacts
   **not yet uploaded** to it — judged by the upload stamp described
   above, so a contact already on the service (even one imported with its
   stamp intact) is correctly treated as done, not offered again. The
   destinations offered are the ones that logbook is bound to.
2. Select the contacts you want to send and upload them to that service.

Uploading is safe to repeat: a contact already on the service is skipped
rather than sent twice (the services de-duplicate anyway). Backfill always
sends one service at a time, and only when you ask — Station Manager never
bulk-uploads your history on its own.

### Slow or unreliable internet

Uploads run in the background and retry on their own, and this is built
for genuinely bad links. A network blip, a rate limit, or an outage is
treated as temporary: the QSO stays queued and is retried later, with the
wait growing between attempts so the service isn't hammered — **for as
long as the connection is gone, whether that's an hour or ten days**. The
moment the link returns, the backlog uploads. (A QSO that a *reachable*
service actively rejects — bad data, wrong credentials — is marked failed
instead, since retrying won't help.) Either way your local log already has
the contact, so forwarding never holds up logging — which is what makes a
no-internet field laptop or a DXpedition viable.

### Advanced: upload pacing

How often each service's uploader checks for new QSOs and how many it
sends at a time are file-only settings, kept per service in the
`forwarders` list of `config.json` (`tick_interval_sec`, `batch_size`, and
`action_filter` for which changes to send). The defaults suit a slow or
unreliable connection and rarely need changing. An action a service can't
perform is refused when the file is loaded — `"update"` for Club Log, say —
and the daemon says which. If you do change them, stop the daemon first:
while it runs it rewrites the whole file from memory whenever a setting
changes in the app.

```
systemctl --user stop smd
# edit ~/.local/share/station-manager/config.json
systemctl --user start smd
```

### Not yet available: LoTW

ARRL Logbook of the World is on the roadmap but not implemented yet.
