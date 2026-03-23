import { types } from '$lib/wailsjs/go/models';

export interface LogbookListState {
    list: types.Logbook[];
}
export const logbookListState: LogbookListState = $state({
    list: [],
});
