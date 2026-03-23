# Station Manager

**Station Manager** is a suite of modern Linux desktop applications for Amateur Radio station management. They are built using
Go, SvelteKit with the Wails framework binding it all together.

Why yet another piece of software for amateur radio logging, etc.? Well, what is out there just didn't allow me to
operate in the way I want to. Also, I have completely abandoned using Windows, and I don't want to use Mac,
so I was left with writing the software myself. Besides, many packages out there, while working, look way
out-of-date, cost too much, and their UI is far too busy to make them easy to set up and a joy to use
(opinionated – as this software is also).

One of the other main requirements is that the software should not require an internet connection to operate.
Here in Malawi, the internet is not always available, and when it is, it is not always reliable. So, the software should
be able to operate without an internet connection. The application will forward QSOs to online logbooks such as QRZ.com
and Station Master (all configurable), but this is not a requirement for the software to operate.

The software is not aimed at contests (although it does support contesting), rather at general HF operation by phone
and CW.

## Computer Aided Transceiver (CAT)

The software does support CAT operation; however, only Yaesu FTdx10 and F7-100 have been tested (I don't own any other
rigs).
