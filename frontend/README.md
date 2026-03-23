# Logging App Frontend

## Frequency Helpers

Use the utility functions in `src/lib/utils/frequency.ts` to convert between
common rig formats:

- `parseCatKHzToMHz("014320000")` → `14.32`
- `formatCatKHzToDottedMHz("014320000")` → `14.320.000`
- `rawKHzStringToDottedMHz("14320")` → `14.320`

`rawKHzStringToDottedMHz` is handy when rendering frequencies read from the
SQLite/PostgreSQL logbook, which stores values as plain kHz strings without
padding or separators.
