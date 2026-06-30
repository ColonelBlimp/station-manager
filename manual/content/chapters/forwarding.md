---
title: Forwarding Your Log (QRZ and ClubLog)
weight: 80
---

Forwarding sends each QSO you log to your online services automatically.
When you log a contact, Station Manager records it locally first — that
never depends on the network — and then, in the background, uploads it to
whichever destinations you have configured. If your internet is slow or
drops out, the upload waits and retries; your logging is never blocked.

Two destinations are supported today: **QRZ.com** and **Club Log**.
(LoTW is planned but not yet available — see the end of this chapter.)

### Where forwarding is configured

Forwarding is set up in `config.json`, in the `forwarders` list. On a
normal install the file lives at:

```
~/.local/share/station-manager/config.json
```

> **Stop the daemon before you edit `config.json`.**
> While Station Manager is running it rewrites the whole file from memory
> whenever a setting changes in the app, which will overwrite edits you
> made by hand. Always: stop the daemon → edit the file → start it again.
>
> ```
> systemctl --user stop smd
> # edit config.json
> systemctl --user start smd
> ```

Each entry in `forwarders` describes one destination:

| Field | Meaning |
|-------|---------|
| `name` | A label you choose (e.g. `"qrz"`). Used in logs and status. |
| `type` | The service: `"qrz"` or `"clublog"`. |
| `enabled` | `true` to upload new QSOs to this service. `false` means *don't queue anything* for it — see "Turning a destination off" below. |
| `credentials` | The login details for that service (differs per type — see below). |
| `action_filter` | Which changes to send: `"insert"`, `"update"`, `"delete"`. Optional — if you leave it out, it defaults to what the service actually supports. |
| `tick_interval_sec` | How often the uploader checks for new QSOs. Default `120` (every 2 minutes). |
| `batch_size` | How many QSOs to send per check. Default `5`. |

The defaults for `tick_interval_sec` and `batch_size` are deliberately
gentle — they suit a slow or unreliable connection. You rarely need to
change them.

### QRZ.com

QRZ needs a single **logbook API key**. Each QRZ logbook has its own key,
found on the logbook's settings page on QRZ.com. That one key both
authenticates you and selects which logbook the QSO lands in.

```json
{
  "name": "qrz",
  "type": "qrz",
  "enabled": true,
  "credentials": {
    "api_key": "XXXX-XXXX-XXXX-XXXX"
  }
}
```

QRZ supports the full lifecycle, so if you later edit or delete a QSO in
Station Manager, that change is forwarded too.

### Club Log

Club Log needs **two separate credentials**, and this trips people up
because the names sound alike:

- **Your account login** — `email` plus `password`. Use a Club Log
  **Application Password** (generate one in your Club Log account
  settings) rather than your main login password, so it can be revoked
  on its own. This is what tells Club Log *whose* log the QSO belongs to.
- **The application API key** — `api`. This identifies *Station Manager
  as a piece of software*, not you. It does **not** replace your login —
  Club Log needs both. You obtain one key at
  [clublog.org/requestapikey.php](https://clublog.org/requestapikey.php).

`callsign` selects which of your account's logs receives the QSO.

```json
{
  "name": "clublog",
  "type": "clublog",
  "enabled": true,
  "credentials": {
    "email": "you@example.com",
    "password": "your-application-password",
    "callsign": "7Q5MLV",
    "api": "your-clublog-application-api-key"
  }
}
```

#### Club Log only uploads and deletes — it does not edit

Club Log's real-time interface can **add** a QSO and **delete** a QSO,
but it cannot change the fields of one already in your log (re-sending an
edited QSO is just treated as a duplicate). Station Manager knows this:
if you leave `action_filter` out, Club Log defaults to
`["insert", "delete"]` automatically — so editing a QSO won't pile up
failed uploads. If you set `action_filter` by hand, don't include
`"update"` for Club Log; the daemon will refuse to start and tell you why.

#### If your credentials are wrong

If Club Log rejects your login, it requires software to **stop sending
immediately** — otherwise your address can be temporarily blocked.
Station Manager honours this: the first rejection halts further Club Log
uploads until you fix the credentials and restart the daemon. So if Club
Log uploads stop, check `email` / `password` / `api`, correct them (with
the daemon stopped), and start it again.

### How to tell a QSO was uploaded

After a successful upload, Station Manager stamps the QSO with the
service's status field — `QRZCOM_QSO_UPLOAD_STATUS` for QRZ,
`CLUBLOG_QSO_UPLOAD_STATUS` for Club Log — set to `Y`, along with the
date. These travel with the QSO in ADIF exports, so you can always see
which contacts have been forwarded. This stamp — not any internal queue —
is the lasting record that a contact reached a service, so it survives
exporting and re-importing your log.

<!-- DRAFT NOTE for the later manual pass — the mechanism below is built and
     working in the daemon (ADR 0038/0039); the logbook-app screens that expose
     it visually are still being built, so describe the workflow once the UI
     lands. Rebuild the embedded manual (cd manual && hugo --quiet --minify)
     when finalising. -->

### Turning a destination off

Setting `enabled: false` means Station Manager **stops queuing** new QSOs
for that service. It is *not* a pause-and-catch-up: any QSOs already
waiting to upload to that service are dropped from the queue (the contacts
themselves are untouched in your log — only the pending upload is
cleared). This is deliberate — it's the clean way to keep a batch of QSOs
(say, a contest) off a service. When you turn the destination back on,
new QSOs queue again, but the ones logged while it was off do **not** get
sent automatically. You send those yourself — see "Catching up" below.

### Catching up: sending past QSOs to a service (backfill)

QSOs logged while a service was disabled, logged before you added it, or
imported from another log won't have been uploaded to that service. You
can send them yourself from the logbook app:

1. Pick the destination (e.g. QRZ) and the app shows you the contacts
   **not yet uploaded** to it — judged by the upload stamp described
   above, so a contact already on the service (even one imported with its
   stamp intact) is correctly treated as done, not offered again.
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

### Not yet available: LoTW

ARRL Logbook of the World is on the roadmap but not implemented yet. Only
`qrz` and `clublog` are valid `type` values today; any other type will be
rejected at startup.
