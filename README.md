<p align="left">
  <img src="assets/logo.png" alt="Station Manager" width="128">
</p>

# Station Manager

**Station Manager** is software for Amateur Radio station management on Linux. It runs as a local daemon (`smd`, written in Go) that serves a browser SPA (Svelte 5 + Vite) for QSO logging, station configuration, and rig control. The daemon and SPA ship as a single binary; the operator points a browser at it.

Why yet another piece of software for amateur radio logging, etc.? Well, what is out there just doesn't allow me to
operate in the way I want to. Also, I don't generally use Windows, and I don't want to use Mac,
so I was left with writing the software myself. Besides, many packages out there, while working, look way
out-of-date, cost too much, and their UIs are far too busy to make them easy to set up and a joy to use
(opinionated – as this software is also).

One of the other main requirements is that the software should not require an internet connection to operate.
Here in Malawi, the internet is not always available, and when it is, it is not always reliable. So, the software should
be able to operate without an internet connection. The application will forward QSOs to online logbooks such as QRZ.com and ClubLog (configurable), but this is not a requirement for the software to operate.

The software is not currently aimed at contesting (although support is planned), rather at general HF operation by SSB, CW, and FT8. There are
plans to support serious contesting, including multiple distributed stations, etc.

Install and first-run setup: see [`docs/install.md`](docs/install.md).

Design decisions, architecture notes, and the ADR log live under `docs/`.

## Computer Aided Transceiver (CAT)

The software does support CAT operation; however, only Yaesu FTdx10, FT-710, and IC7300 have been tested (I don't own any other
rigs).

## Licence

Station Manager is licensed under the **GNU General Public License, version 3 only** (`GPL-3.0-only`) — see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

It was MIT-licensed until 2026-05-31. The move to GPL-3.0-only follows from the FT8 decode path: that capability comes from the companion library [go-ft8](https://github.com/ColonelBlimp/go-ft8), a WSJT-X/jt9-derived work that is GPL-3.0-only. Linking it makes the combined work a GPLv3 derivative, so the whole project adopts the same copyleft licence. The reasoning is recorded in [`docs/decisions/0023-relicense-to-gplv3.md`](docs/decisions/0023-relicense-to-gplv3.md) and [`docs/licensing.md`](docs/licensing.md).
