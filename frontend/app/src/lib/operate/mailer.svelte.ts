// Mailer projection (daemon-managed, read-only) for the Export dialog's email
// path. Injected from main.ts at boot (setMailer, fed by fetchStationContext)
// so the dialog component never imports the config fetch — same seam pattern
// as setModeMappings / setMyGrid.

export const mailer: { enabled: boolean; defaultRecipient: string } = $state({
    enabled: false,
    defaultRecipient: '',
});

export function setMailer(enabled: boolean, defaultRecipient: string): void {
    mailer.enabled = enabled;
    mailer.defaultRecipient = defaultRecipient;
}
