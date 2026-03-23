import { LogError } from '$lib/wailsjs/runtime';
import { showToast } from '$lib/utils/toast';

export const handleAsyncError = (error: unknown, operation: string): void => {
    const errMsg = error instanceof Error ? error.message : String(error);
    LogError(`${operation}: ${errMsg}`);
    showToast.ERROR(errMsg);
};
