import { CONTEST_TIMER_INTERVAL_MS } from '$lib/constants/timers';
import { formatTimeSecondsToHHColonMMColonSS } from '@station-manager/shared-utils';

export interface ContestTimers {
    sinceStartTimerID: number | null;
    sinceLastQsoTimerID: number | null;
    elapsedSinceStart: number;
    elapsedSinceLastQso: number;
    formattedSinceStart: string;
    formattedSinceLastQso: string;
    isRunning: boolean;
    start(): void;
    reset(): void;
    stop(): void;
}

export class ContestTimersClass implements ContestTimers {
    sinceStartTimerID: number | null;
    sinceLastQsoTimerID: number | null;
    elapsedSinceStart: number = $state(0);
    elapsedSinceLastQso: number = $state(0);
    isRunning: boolean = $state(false); // Add a reactive flag

    // Derived formatted values - only recompute when elapsed values change
    formattedSinceStart: string = $derived(
        formatTimeSecondsToHHColonMMColonSS(this.elapsedSinceStart)
    );
    formattedSinceLastQso: string = $derived(
        formatTimeSecondsToHHColonMMColonSS(this.elapsedSinceLastQso)
    );

    constructor() {
        this.sinceStartTimerID = null;
        this.sinceLastQsoTimerID = null;
        this.elapsedSinceStart = 0;
        this.elapsedSinceLastQso = 0;
    }
    start(): void {
        if (this.sinceStartTimerID === null) {
            this.isRunning = true;
            this.sinceStartTimerID = window.setInterval(() => {
                this.elapsedSinceStart += 1;
            }, CONTEST_TIMER_INTERVAL_MS);
        }
    }
    reset(): void {
        // Always clear existing timer if running
        if (this.sinceLastQsoTimerID !== null) {
            window.clearInterval(this.sinceLastQsoTimerID);
            this.sinceLastQsoTimerID = null;
        }
        this.elapsedSinceLastQso = 0;
        this.sinceLastQsoTimerID = window.setInterval(() => {
            this.elapsedSinceLastQso += 1;
        }, CONTEST_TIMER_INTERVAL_MS);
    }
    stop(): void {
        if (this.sinceStartTimerID !== null) {
            window.clearInterval(this.sinceStartTimerID);
            this.sinceStartTimerID = null;
        }
        if (this.sinceLastQsoTimerID !== null) {
            window.clearInterval(this.sinceLastQsoTimerID);
            this.sinceLastQsoTimerID = null;
        }
        this.elapsedSinceStart = 0;
        this.elapsedSinceLastQso = 0;
        this.isRunning = false;
    }
}

export const contestTimers: ContestTimers = new ContestTimersClass();
