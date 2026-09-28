---
title: Enrichment (looking up the stations you work)
weight: 85
---

When you log a contact, Station Manager fills in what it can about the other
station — its country and zones, name, locator and address — from the lookup
sources set up in **Settings → Enrichment**. Lookups never block logging: if a
source is slow or unreachable, the contact is logged with whatever is known.

Each source is listed with a short summary; open it to switch it on or off and,
for a source that needs an account, enter your login. A saved password shows as
saved, with **Replace** to change it. Changes apply after a restart: when you
save, the tab says *Saved changes apply after a restart* with a **Restart
daemon** button; press it when you are ready.

### Hamnut {#lookup-hamnutlookupservice}

Works out the country and the CQ and ITU zones from the callsign's prefix. Free,
and needs no account.

### QRZ.com {#lookup-qrzlookupservice}

Fills in the name, locator and address from QRZ.com. Needs a QRZ.com
subscription that includes XML data access; enter your QRZ.com username and
password.

### QRZCQ.com {#lookup-qrzcqlookupservice}

Fills in the name, locator and address from QRZCQ.com. Needs a premium QRZCQ
account with XML access; enter your QRZCQ username and password.

### Fill gaps from other sources

The callsign sources are asked in **priority** order, 1 first. When a detail
you have ticked here — **Name** or **Locator** — is still empty after a source
answers, the next source is asked too. It fills any other empty details it
knows as well, but never replaces what a higher-priority source already gave.

### How long to keep looked-up details

Station Manager remembers what it looked up, so working the same station again
needs no new lookup. **Country details** and **Station details** say how many
days that remembered information counts as fresh; after that it is looked up
again the next time it is needed. Leave a box empty to use the default shown
(365 days for country details, 90 for station details). A **0** means the
details never go stale: they are kept until something replaces them.

**Lookups at once** is how many of those refreshes may run at the same time
(4 if left empty).
