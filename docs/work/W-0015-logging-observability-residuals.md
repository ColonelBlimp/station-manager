# W-0015 — Apply logging-audit residuals with adjacent work

**Status:** Deferred — audit closed; adjacent-work only
**Selected:** Not selected
**Outcome:** Three low-urgency observability gaps close when their owning code is already open,
without another repository-wide logging sweep.

## Residuals

- **F14:** preserve QRZ/ClubLog accepted-upstream disposition in `Result.Detail` instead of
  collapsing it to generic forwarding success; do this when either forwarder is next changed.
- **L6:** add `request_id` to direct handler warning/error breadcrumbs as those handlers are changed;
  the established 5xx and panic paths already carry correlation.
- **F13:** add one bounded boot warning for a keyless ClubLog build when ClubLog construction or
  configuration is next changed; the first-outage warning remains the runtime fallback.

FT8-11 belongs to the clean-opening work in
[W-0011](W-0011-ft8-and-rig-refinements.md). Q7 is fixed and is not open work.

## Verification boundary

Tests assert structured fields and bounded emission at the logging boundary. Do not expose
credentials, third-party response bodies, or uncontrolled values, and do not schedule a standalone
multi-handler edit merely to close this dossier.

## References

- [`internal-codebase-logging-gaps.md`](../reviews/internal-codebase-logging-gaps.md) — closed audit
  and durable finding IDs.
