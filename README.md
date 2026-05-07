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

The software is not currently aimed at contesting (although it does support it), rather at general HF operation by SSB and CW. There are
plans to support serious contesting, including multiple distributed stations, etc.

Design decisions, architecture notes, and the ADR log live under `docs/`.

## Computer Aided Transceiver (CAT)

The software does support CAT operation; however, only Yaesu FTdx10 and FT-710 have been tested (I don't own any other
rigs).
