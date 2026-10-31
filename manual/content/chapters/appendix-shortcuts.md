---
title: "Appendix: Keyboard Shortcuts"
weight: 200
appendix: true
---

This manual is self-contained: the full shortcut tables live here, not behind a
link, so they're available offline and from disk.

Station Manager is built for keyboard-first logging. Logging keys and rig keys
never share a key: logging uses <kbd>Tab</kbd>, <kbd>Enter</kbd>,
<kbd>Esc</kbd> and the function keys, and rig control is always
<kbd>Ctrl</kbd>+<kbd>Shift</kbd>+key.

While an edit panel is open it owns the keyboard: the logging and rig
shortcuts below do nothing until it closes. While an archive switch is under
way, entry is locked and the logging shortcuts do nothing either.

## Logging a contact (Operate → Phone / CW)

| Keys | Where | What it does |
|---|---|---|
| <kbd>Tab</kbd>, <kbd>Enter</kbd> or <kbd>Space</kbd> | Callsign field | Commits the callsign and starts the contact: stamps Time On, starts the QSO timer (the ticking Time Off), looks the station up, and opens the Worked panel if you've worked it before. <kbd>Enter</kbd> and <kbd>Space</kbd> commit only a valid callsign; <kbd>Space</kbd> never types a space. <kbd>Tab</kbd> also moves to the next field. Committing again after correcting a typo keeps the original Time On. |
| <kbd>Ctrl</kbd>+<kbd>Enter</kbd> | Anywhere on the card | Logs the contact. On success the card clears and the cursor returns to Callsign for the next one; if logging is refused, the cursor stays where you are so you can fix it. |
| <kbd>Esc</kbd> | Anywhere on the card | Clears the contact and starts again at Callsign. |
| <kbd>Shift</kbd>+<kbd>Enter</kbd> | Callsign field, or no field focused | Sets the typed callsign aside on the pile-up stack and clears the contact. (The ≡ button in the Callsign field does the same.) In the Notes box it is an ordinary new line. |
| <kbd>Shift</kbd>+<kbd>↑</kbd> | No text field focused | Takes the **newest** callsign off the pile-up stack into the Callsign field. |
| <kbd>Shift</kbd>+<kbd>↓</kbd> | No text field focused | Takes the **oldest** callsign off the pile-up stack into the Callsign field. |
| <kbd>F2</kbd> | Anywhere on the card | Opens the Worked panel for the typed callsign **without** starting the contact or its timer. |
| <kbd>F3</kbd> | Anywhere on the card | With the timer running: freezes Time Off (the contact has ended; you're still typing details). With a callsign typed and no contact started: starts the contact's timer. Holding the key down does not toggle it repeatedly. |

### In contest mode

With **Contest** ticked in the header:

| Keys | What changes |
|---|---|
| <kbd>Tab</kbd> from Callsign | Also allocates the Sent # and checks for a duplicate on this band in this contest. |
| <kbd>Tab</kbd> order | Callsign → Rcvd # → District. RST Sent, RST Rcvd and Sent # are skipped (contest reports are 59 and the serial is allocated) but stay clickable. |

## Rig control (Operate, in Phone / CW and FT8)

These work wherever the Operate view is open, on both Phone / CW and FT8. The
band and VFO keys need a CAT-connected rig that supports them; otherwise they
do nothing.

| Keys | What it does |
|---|---|
| <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>↑</kbd> / <kbd>↓</kbd> | Frequency up / down 100 Hz. |
| <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>→</kbd> / <kbd>←</kbd> | Frequency up / down 10 Hz. |
| <kbd>Ctrl</kbd>+<kbd>Alt</kbd>+<kbd>Shift</kbd>+<kbd>↑</kbd> / <kbd>↓</kbd> | Frequency up / down 5 kHz. |
| <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>]</kbd> / <kbd>[</kbd> | Next / previous band (the rig's band-stack). |
| <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>1</kbd> … <kbd>9</kbd>, <kbd>0</kbd> | Jumps to the 1st … 10th band of your operating bands list (by default 160, 80, 60, 40, 30, 20, 17, 15, 12, 10 m). |
| <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>\\</kbd> | Swaps VFO A and VFO B. |

The frequency keys stand down while you're typing in a text field, where
<kbd>Ctrl</kbd>+<kbd>Shift</kbd>+arrow selects words as usual. The band and
VFO keys work whatever has focus.

## Editing a logged contact (Logbook → Edit, Session → Edit)

| Keys | What it does |
|---|---|
| <kbd>Ctrl</kbd>+<kbd>Enter</kbd> (<kbd>⌘</kbd>+<kbd>Enter</kbd> on a Mac) | Saves the edit. |
| <kbd>Esc</kbd> | Cancels and closes the edit panel. |

Neither works while a save is in progress, and Save stays unavailable while
the contest exchange in the Contest section is invalid.

## Smaller pop-ups

| Keys | Where | What it does |
|---|---|---|
| <kbd>Esc</kbd> | Comment history list (Phone / CW) | Closes the list only; your contact is not cleared. |
| <kbd>Enter</kbd> | FT8 Band Activity filter | Closes the filter (it applies as you type). |
| <kbd>Esc</kbd> | FT8 Band Activity filter | Closes the filter. |
| <kbd>Esc</kbd> | FT8 pile-up drawer | Closes the drawer. |

## FT8 / FT4 transmit offset (the occupancy spectrum)

With the spectrum's offset bar focused:

| Keys | What it does |
|---|---|
| <kbd>→</kbd> or <kbd>↑</kbd> | Moves the transmit offset up by about a tenth of a signal width. |
| <kbd>←</kbd> or <kbd>↓</kbd> | Moves the transmit offset down by about a tenth of a signal width. |
| <kbd>Home</kbd> | Moves to the bottom of the passband. |
| <kbd>End</kbd> | Moves to the top of the passband. |

With no offset chosen yet, the keys start from the offset Station Manager
recommends.
