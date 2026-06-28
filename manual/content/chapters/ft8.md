---
title: Operating FT8
weight: 70
---

> **Station Manager operates FT8 attended only.** You decide whom to work and
> arm each transmission; SM never calls or answers on its own. Unattended or
> automatic operation is not supported.

*Draft outline — content to be written.*

- Enabling FT8 and what's required.
- Choosing your audio device.
- Band Activity: reading decodes, country flags, and beam headings.
- Answering a CQ: pick a clear transmit offset, click a CQ, and let the exchange auto-advance to 73.
- Calling CQ and working answerers.
- The Session tab and where your FT8 QSOs go.

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
