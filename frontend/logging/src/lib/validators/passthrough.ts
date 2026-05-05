/**
 * Passthrough validator — accepts anything, including empty.
 *
 * Use for free-text `ValidatedInput`s where the format isn't
 * constrained client-side (e.g. operator name, antenna description,
 * morse-key info). The daemon performs the authoritative
 * format-correct string check on PUT, so the SPA-side validator
 * doesn't earn its keep beyond satisfying ValidatedInput's
 * `validator: (v: string) => boolean` prop.
 *
 * When a real format constraint emerges (zone ranges, altitude as
 * a signed decimal, etc.), introduce a per-field validator under
 * `lib/validators/<field>.ts` and replace the passthrough at that
 * call site only.
 */
export const passthrough = (): boolean => true;
