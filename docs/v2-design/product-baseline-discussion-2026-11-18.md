# Discussion record: the baseline product, the schema roll-up and themes (2026-11-18)

**Status:** A record of a discussion between the operator and Claude on 2026-11-18. It is not a
plan, a brief or a ruling, and it ranks nothing: [the backlog](../backlog.md) alone owns
priority. Where the operator's words are quoted they are the operator's; everything else is
Claude's reading or suggestion and is labelled so. The operator asked for it to be written down
("Just record all of this for now in a 'discussion' type document. Then we'll continue with
what we are working on").

## Where the operator wants to get to

The operator:

> Where I want to get to is to a basic stable, polished UI product on which we can do some
> serious code reviews and minimisation (remove all the dross), before we start adding new
> features (RTTY, CW, etc). Including all the db migrations should be rolled up into one schema
> file - we only need migrations when we have other installs, and we only have one other install
> and that can be handled manually.

Asked where the line of that basic product lies, the operator:

> Session is inside the line, keep forward-only migrations, FT8/4 tweaks (such as the fox & hound
> work from yesterday), UI tweaks and polish (inc themes)

So, as Claude understands it, the basic product is:

1. the permanent operating-client check and the removals ruled the same day (W-0027, C2e);
2. one baseline schema file, with forward-only migrations kept for later changes;
3. the daemon-owned Session ([W-0029](../work/W-0029-operating-sessions.md)), with the window
   set and the close flow that wait on it (W-0027 A2, A3);
4. FT8 and FT4 adjustments, such as the call-the-addressee work of 2026-11-17;
5. UI adjustments and polish, including themes.

After that come the code reviews and the minimisation, and only then new modes such as RTTY and
CW.

## The schema roll-up

- **The operator's direction:** roll the database migrations into one schema file.
- **Claude's point, which the operator accepted ("keep forward-only migrations"):** migrations
  do two jobs. One is carrying other people's installs forward, and that is not needed now. The
  other is changing the operator's own live log when the schema changes, and that still is:
  without the mechanism, each later change is run by hand on the file that holds the contacts.
- **So, as discussed:** squash the nineteen log migrations into one baseline; drop every
  "down" file with the downgrade tooling (its removal is ruled in W-0027); keep a forward-only
  mechanism for what comes after.
- **Suggested by Claude, not confirmed:** an install that already exists, the operator's and the
  one other known, is marked by hand, once, as already at the baseline; and the config file's
  version history is squashed the same way.

## Order (Claude's suggestion; not ruled)

- Bring forward only the removals that coming work would otherwise be built on: the permanent
  check, the downgrade tooling, and the schema roll-up, before the Session adds to the schema.
- Leave the general sweep for the review phase, once the product is stable. Its first step would
  be an inventory of the dross with sizes, reviewed before anything is deleted, since some of it
  is still leaned on.
- FT8/FT4 and UI adjustments are small and are done when the operator asks for one.

## Themes

The operator: "Polishing the existing light and dark themes and adding some more themes to the
mix", with nine screenshots of another product's dashboard, one per theme of its selector:
Dark, Dark Contrast, Midnight Blue, Graphite, Burgundy, UV Green, Purple, Orange and Light. The
screenshots are on the operator's machine (`Screenshot_20261118_0920*.png` to
`Screenshot_20261118_0923*.png`) and are not in the repository: they show another product and
other operators.

**What the screenshots show** (Claude's reading).
- The nine are one layout in nine colourings. From theme to theme only a few things change: the
  page background, a dark tinted gradient; the panel's surface and its thin border; the colour
  of the small uppercase labels and of links.
- The same in every theme: white numerals, the green "live" dot, the orange "reconnecting" dot
  and the gauge's colours. Status colours are not themed.
- Separate from colour, the look also has large rounded cards, translucent panels and
  letter-spaced uppercase labels.

**What Station Manager has today** (measured in `frontend/app` on 2026-11-18).
- `src/styles/app.css` defines 26 colour tokens (canvas, surface, line, ink, muted, the
  occupancy and map colours, and others) with a light set and one dark override
  (`html[data-theme='dark']`).
- The theme is a two-way toggle (`ui.theme`, stored as `sm-theme`; `src/lib/ui/state.svelte.ts`).
  The `dark:` variant follows `data-theme='dark'`.
- 161 uses of raw palette classes in 26 of the 61 components, not through tokens: amber 52,
  green 44, red 40, gray 10, sky 8, zinc 3, emerald 2, blue 2. Most are status colours.

**What adding themes would take** (Claude's assessment).
1. A further block of token values for each theme, and a selector in place of the toggle.
2. "Dark" as a family: the `dark:` variant has to hold for every dark theme, not for one.
3. Contrast on tinted backgrounds: red on Burgundy, green on UV Green, amber on Orange. TX armed,
   the alarms and the FT8 occupancy colours must stay unmistakable in every theme, so each theme
   is checked against those states and against the occupancy fixture, not only against a screen
   that looks pleasant.
4. The map's land and water colours for each theme.

**Not to be copied** (Claude's view): the Light theme's header, whose title and callsign are
nearly invisible against the photographic background; and the starfield and photographic
backgrounds themselves, which are that product's artwork and make contrast harder to guarantee.

**A standing decision this touches.** W-0004 closed on 2026-08-27 with one light and dark palette
and "named palettes are declined", and [the capsule](../current.md) lists that among the
decisions not to revisit. Adding themes reopens it. The capsule is not changed by this record;
it is to be changed when the operator rules on themes.

**Open, for the operator.**
1. Palette only, or the styling too (rounded cards, translucent panels, letter-spaced labels)?
2. Which themes first? Claude suggested proving the mechanism with two, for example Midnight
   Blue and Graphite, and adding the rest one at a time.

## CI time (added later the same day)

The operator, after seeing how long a run takes: "We should look at the CI later as you
suggest, for 30min per push is heavy and slows the whole dev cycle".

**Measured** from hosted run 44250170450 (green, on `059f0568`): its steps total 29.0 minutes.

| Step | Minutes |
|---|---|
| `go test` with the race detector | 9.6 |
| Desktop shell scenarios in Electron | 6.0 |
| Desktop app RPM: build, content check, smoke check | 3.1 |
| `go test`, full, no race | 1.8 |
| Maintainability observatory | 1.7 |
| App SPA lint, tests and install | 3.3 |
| Everything else | about 3.5 |

**Claude's reading, not a plan.** The removals in hand (the check's off state, the second
real-daemon scenario run, the load generator's build, later the downgrade tests) should save
a minute or two, no more: the two large steps mostly test behaviour that stays. The larger
levers are the race-detector step, which runs the whole Go suite in one go, and the Electron
step, which runs its scenarios one after another. Neither has been examined. This belongs
with the review and minimisation phase above; nothing is scheduled by this note.

## What this record changes

Nothing. No priority, no dossier and no standing note is changed by it. The work in hand when it
was written was the removal of the operating-client switch (W-0027, C2e), awaiting Codex's
verification.
