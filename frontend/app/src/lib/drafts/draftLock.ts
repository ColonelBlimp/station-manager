// Shared by Restore and Discard. Hold the callback until the owner explicitly
// releases; view unmounting never releases a claim (ADR 0085 RS8–RS11).
// https://w3c.github.io/web-locks/
export interface DraftReservation {
    release(): Promise<void>;
}

export function draftLocksAvailable(): boolean {
    return typeof navigator !== 'undefined' && typeof navigator.locks?.request === 'function';
}

export function reserveSavedDraft(id: string): Promise<DraftReservation> {
    return new Promise((resolve, reject) => {
        if (!draftLocksAvailable()) {
            reject(new Error('This page cannot reserve the QSO for one tab.'));
            return;
        }
        let finish!: () => void;
        const held = new Promise<void>((done) => {
            finish = done;
        });
        const released = navigator.locks.request(
            `station-manager.saved-qso.${id}`,
            { mode: 'exclusive', ifAvailable: true },
            async (lock) => {
                if (lock === null) throw new Error('In use in another tab.');
                resolve({
                    release: async () => {
                        finish();
                        await released;
                    },
                });
                await held;
            }
        );
        // Rejection includes denied access and a null ifAvailable result. It
        // must reach the caller without leaving an unhandled request promise.
        void released.catch(reject);
    });
}
