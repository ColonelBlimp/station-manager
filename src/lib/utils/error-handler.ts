/**
 * Generic async error handler factory.
 * Each app can configure this with its own logging and toast implementations.
 */
export type ErrorLogger = (message: string) => void;
export type ErrorNotifier = (message: string) => void;

export interface ErrorHandlerConfig {
    logger: ErrorLogger;
    notifier: ErrorNotifier;
}

export function createErrorHandler(config: ErrorHandlerConfig) {
    return (error: unknown, operation: string): void => {
        const errMsg = error instanceof Error ? error.message : String(error);
        config.logger(`${operation}: ${errMsg}`);
        config.notifier(errMsg);
    };
}

/**
 * Extract error message from unknown error type.
 */
export function getErrorMessage(error: unknown): string {
    return error instanceof Error ? error.message : String(error);
}

