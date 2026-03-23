import { types } from '$lib/wailsjs/go/models';
import { CONTEST_TIMER_INTERVAL_MS } from '$lib/constants/timers';

export interface SessionState {
    operator: string;
    total: number;
    list: types.Qso[];
    update(this: SessionState, list: types.Qso[]): void;
    start(this: SessionState): void;
    stop(this: SessionState): void;
    reset(this: SessionState): void;
}

export interface SessionTimeState {
    elapsed: number;
}

let sessionIntervalID: number | null = null;

// Cleanup function to be called when the state is no longer needed
export const cleanupSessionState = (): void => {
    if (sessionIntervalID !== null) {
        window.clearInterval(sessionIntervalID);
        sessionIntervalID = null;
    }
};

// Declare sessionTimeState before sessionState to avoid forward reference
export const sessionTimeState: SessionTimeState = $state({
    elapsed: 0,
});

export const sessionState: SessionState = $state<SessionState>({
    operator: '',
    total: 0,
    list: [],
    update(list: types.Qso[]): void {
        this.list = list;
        this.total = list.length;
    },
    start(): void {
        // Always cleanup any existing timer before starting a new one
        cleanupSessionState();
        sessionTimeState.elapsed = 0;
        sessionIntervalID = window.setInterval(() => {
            sessionTimeState.elapsed += 1;
        }, CONTEST_TIMER_INTERVAL_MS);
    },
    stop(): void {
        cleanupSessionState();
    },
    reset(): void {
        cleanupSessionState();
        sessionTimeState.elapsed = 0;
        this.list = [];
        this.total = 0;
    },
});

// Helper function to format seconds as HH:MM:SS
export const formatSeconds = (totalSeconds: number): string => {
    const seconds: number = Math.max(0, totalSeconds);
    const hours: number = Math.floor(seconds / 3600);
    const minutes: number = Math.floor((seconds % 3600) / 60);
    const secs: number = seconds % 60;

    return [hours, minutes, secs].map((v) => v.toString().padStart(2, '0')).join(':');
};

// Function to get formatted elapsed time
export const getSessionElapsedTime = (): string => {
    return formatSeconds(sessionTimeState.elapsed);
};
