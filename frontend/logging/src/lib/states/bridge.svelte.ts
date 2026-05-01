/**
 * Bridge SSE transport state.
 *
 * Tracks the SPA's view of the bridge connection. Per ADR 0010
 * (`docs/decisions/0010-rig-sse-wire-shape.md`):
 *
 *   - `connected` mirrors `EventSource.readyState === OPEN` —
 *     the SSE channel is open. Transport-level only; says nothing
 *     about the rig's responsiveness.
 *   - `rigResponding` is `false` until the first `rig-state` event
 *     arrives, and `false` after a `rig-disconnected` event. Any
 *     `rig-state` event flips it back to `true` (implicit reconnect).
 *
 * Both flags are read by `displayedState` to decide whether to read
 * from `catState` (live rig) or `manualState` (operator edits).
 *
 * **Stub for now.** The real EventSource wiring lands in a later
 * implementation step. Both flags default to `false`, so the SPA
 * runs in CAT-off mode (manualState drives displayedState).
 *
 * See ADR 0009 (`docs/decisions/0009-cat-state-decomposition.md`)
 * for the four-object decomposition this fits into.
 */

class BridgeState {
    connected: boolean = $state(false);
    rigResponding: boolean = $state(false);
}

export const bridgeState = new BridgeState();
