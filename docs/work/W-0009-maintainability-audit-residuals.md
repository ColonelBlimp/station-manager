# W-0009 — Retire verified maintainability and test-architecture residuals

**Status:** Deferred — adjacent-change work except where a trigger is named
**Selected:** Not selected
**Outcome:** Package boundaries, build variants, generated artifacts, and browser behavior gain
focused guards without creating speculative frameworks or broad cleanup commits.

## Routed residuals

- **Packaging (alpha.2 dogfood Finding #17):** the nfpm spec declares the manual files but not their
  directories, so `dnf remove` leaves `/usr/share/doc/station-manager/manual` behind, unowned; declare the
  directories with the next packaging change.
- **Package boundaries (PB-1..4):** add missing stdlib-only, FT8-direction, and cloud-independence
  guards; reduce lookup/smcloud leaf ownership only when a concrete change opens the boundary; drop
  the dead exported `utils.DXCCFromISO2` when its package is next touched.
- **Maintainability/testing (MT-1, MT-3..7):** fix the opt-in `logging_debug` nil panic; retire the
  four production duplicate pairs now held by the exact CI ratchet; retire context-free SQLite
  wrappers and test-only production machinery incrementally; make model generation reproducible;
  add missing focused `httpkit` and hardware leaf tests.
- **Browser proof (F-05/F-07):** make modals own focus, keyboard, and Escape behavior and establish
  the first browser/a11y layer. RF shortcuts must remain stood down while a modal owns input.
- **Frontend hydration (F-08):** validate session members, not only the outer array.
- **Needs-trigger:** LC-5 matters only if SQLite `Open`/`Close` become concurrent; ST-3b remains
  governed by ADR 0069 and its explicit insecure-network acknowledgement.

## Verification boundary

Each change must remove or tighten a named gap and preserve existing package ownership. Source-shape
guards need an adjacent comment explaining the invariant they protect. Browser tests must prove the
actual focus/keyboard behavior, not only component state. No item here authorizes a new lifecycle,
DI, transport, or test framework.

## References

- [`internal-package-boundary-audit.md`](../reviews/internal-package-boundary-audit.md)
- [`internal-maintainability-test-architecture-audit.md`](../reviews/internal-maintainability-test-architecture-audit.md)
- [`frontend-app-review.md`](../reviews/frontend-app-review.md)
- [`ADR 0069`](../decisions/0069-non-loopback-tcp-acknowledgement.md)
