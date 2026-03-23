import { LogError } from '$lib/wailsjs/runtime/runtime';
import { showToast } from '$lib/utils/toast';
import { createErrorHandler } from '@station-manager/shared-utils';

export const handleAsyncError = createErrorHandler({
    logger: LogError,
    notifier: showToast.ERROR,
});
