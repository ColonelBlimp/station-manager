export interface ContestState {
    isIncremental: boolean;
    totalQsos: number;
    currentStx: string;
    srxInvalid: boolean;
    setCurrent(stx: string): void;
    increment(): string;
    setSrxInvalid(invalid: boolean): void;
    resetSrxValidation(): void;
}

export const contestState: ContestState = $state<ContestState>({
    isIncremental: true,
    // The total number of QSOs logged so far
    totalQsos: 0,
    currentStx: '001', // Default starting value
    srxInvalid: false, // Tracks validation state of SRX input
    // Set the current STX value
    setCurrent(this: ContestState, stx: string): void {
        let increment = true;
        let next = stx.trim();
        if (next.startsWith('!')) {
            increment = false;
            next = next.substring(1);
        }
        this.isIncremental = increment;
        this.currentStx = next;
    },
    // Get the next STX value
    increment(this: ContestState): string {
        if (this.isIncremental) {
            this.currentStx = doIncrement(this.currentStx);
        }
        return this.currentStx;
    },
    // Set SRX validation state
    setSrxInvalid(this: ContestState, invalid: boolean): void {
        this.srxInvalid = invalid;
    },
    // Reset SRX validation to valid state
    resetSrxValidation(this: ContestState): void {
        this.srxInvalid = false;
    },
});

const doIncrement = (cur: string): string => {
    const next = parseInt(cur, 10) + 1;
    if (Number.isNaN(next)) {
        return cur;
    }
    const nextStr = next.toString();
    const width = Math.max(cur.length, nextStr.length);
    return nextStr.padStart(width, '0');
};
