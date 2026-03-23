export function addRecent<T>(list: readonly T[], item: T, maxLength: number): T[] {
    // Treat list and maxLength as read-only by convention.
    // Returns a new list with `item` moved to the front and pruned to `maxLength`.

    if (maxLength <= 0) return [];

    const withoutItem = list.filter((x) => x !== item);
    const next = [item, ...withoutItem];
    return next.slice(0, maxLength);
}
