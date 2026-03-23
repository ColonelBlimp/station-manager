import { types } from '$lib/wailsjs/go/models';

export interface LogbookState {
    id: number;
    name: string;
    callsign: string;
    description: string | undefined;
    clear(this: LogbookState): void;
    fromLogbook(this: LogbookState, lb: types.Logbook): void;
    toLogbook(this: LogbookState): types.Logbook;
}

export const logbookState: LogbookState = $state({
    id: 0,
    name: '',
    callsign: '',
    description: '',
    clear(this: LogbookState): void {
        this.id = 0;
        this.name = '';
        this.callsign = '';
        this.description = '';
    },
    fromLogbook(this: LogbookState, lb: types.Logbook): void {
        this.id = lb.id;
        this.name = lb.name;
        this.callsign = lb.callsign;
        this.description = lb.description ?? '';
    },
    toLogbook(this: LogbookState): types.Logbook {
        const base = new types.Logbook();
        base.id = this.id;
        base.name = this.name;
        base.callsign = this.callsign.toUpperCase();
        base.description = this.description;
        return base;
    },
});
