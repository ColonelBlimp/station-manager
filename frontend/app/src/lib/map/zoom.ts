/**
 * Map zoom/pan transform + endpoint hit-testing — pure math, no DOM
 * (backlog "Contacts map — zoom/pan + station hover tooltip").
 *
 * The transform lives in VIEWBOX coordinates: WorldMap wraps its drawing
 * in `translate(x, y) scale(k)`, so content coordinates (what the engine
 * projects) map to viewBox coordinates as `v = c * k + t`. Hit-testing
 * runs in content space via toContent — the reason zoom and the hover
 * tooltip ship as one item: a hit-test built against the static
 * projection breaks the moment a transform lands.
 */

export interface ZoomTransform {
    k: number;
    x: number;
    y: number;
}

export const IDENTITY: ZoomTransform = { k: 1, x: 0, y: 0 };

/** Zoom bounds: 1 = the fitted whole-world view (zooming out past the
 *  fit just shrinks the map into letterbox space — never useful). */
export const MIN_ZOOM = 1;
export const MAX_ZOOM = 16;

function clamp(v: number, lo: number, hi: number): number {
    return Math.min(Math.max(v, lo), hi);
}

/**
 * Clamp scale to the zoom bounds and translation so the scaled content
 * always covers the viewBox (no dragging the world off-screen). At k = 1
 * the only legal translation is zero, so the identity is returned —
 * which also makes "zoomed out fully" === IDENTITY, a stable reset check.
 */
export function clampTransform(t: ZoomTransform, w: number, h: number): ZoomTransform {
    const k = clamp(t.k, MIN_ZOOM, MAX_ZOOM);
    if (k === 1) return { ...IDENTITY };
    return { k, x: clamp(t.x, w * (1 - k), 0), y: clamp(t.y, h * (1 - k), 0) };
}

/**
 * Scale by `factor` about the viewBox point (px, py) — the content under
 * the cursor stays under the cursor, the way every slippy map zooms.
 */
export function zoomAt(
    t: ZoomTransform,
    px: number,
    py: number,
    factor: number,
    w: number,
    h: number
): ZoomTransform {
    const k = clamp(t.k * factor, MIN_ZOOM, MAX_ZOOM);
    if (k === t.k) return t;
    return clampTransform(
        { k, x: px - ((px - t.x) * k) / t.k, y: py - ((py - t.y) * k) / t.k },
        w,
        h
    );
}

/** Pan by a viewBox-space delta (pointer drag), staying in bounds. */
export function panBy(
    t: ZoomTransform,
    dx: number,
    dy: number,
    w: number,
    h: number
): ZoomTransform {
    if (t.k === 1) return t;
    return clampTransform({ k: t.k, x: t.x + dx, y: t.y + dy }, w, h);
}

/** ViewBox point → content (pre-transform projection) coordinates. */
export function toContent(t: ZoomTransform, px: number, py: number): [number, number] {
    return [(px - t.x) / t.k, (py - t.y) / t.k];
}

export interface EndPoint {
    x: number;
    y: number;
}

/**
 * Indices of every endpoint stacked at the one nearest (cx, cy) —
 * empty when the nearest is farther than hitR (no hover). Contacts at
 * the same grid project to the same pixel, so the tooltip must list the
 * whole stack (groupR), not just the topmost circle; zooming in spreads
 * near-neighbours apart, which is the other reason zoom + tooltip pair.
 * All arguments are content-space; callers divide screen radii by k.
 */
export function endpointsNear(
    ends: readonly EndPoint[],
    cx: number,
    cy: number,
    hitR: number,
    groupR: number
): number[] {
    let best = -1;
    let bestD = Infinity;
    for (let i = 0; i < ends.length; i++) {
        const d = Math.hypot(ends[i].x - cx, ends[i].y - cy);
        if (d < bestD) {
            bestD = d;
            best = i;
        }
    }
    if (best < 0 || bestD > hitR) return [];
    const b = ends[best];
    const out: number[] = [];
    for (let i = 0; i < ends.length; i++) {
        if (Math.hypot(ends[i].x - b.x, ends[i].y - b.y) <= groupR) out.push(i);
    }
    return out;
}
