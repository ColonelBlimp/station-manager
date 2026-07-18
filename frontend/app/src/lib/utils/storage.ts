/**
 * Fail-soft localStorage access. Several preference reads run at MODULE INIT
 * (theme, nav collapse, route mode), where a browser that throws on storage
 * access (disabled cookies/storage, sandboxed iframe → SecurityError) would
 * otherwise kill the whole app boot on a blank page. Preferences are never
 * worth that: a throw degrades to "no stored value" / "not persisted" and the
 * session just runs on defaults.
 */

export function storageGet(key: string): string | null {
    try {
        return localStorage.getItem(key);
    } catch {
        return null;
    }
}

export function storageSet(key: string, value: string): void {
    try {
        localStorage.setItem(key, value);
    } catch {
        // Not persisted — acceptable for a preference.
    }
}

export function storageRemove(key: string): void {
    try {
        localStorage.removeItem(key);
    } catch {
        // Already effectively absent.
    }
}
