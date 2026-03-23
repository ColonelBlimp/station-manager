import { derived, writable, type Readable, type Writable } from 'svelte/store';

// Define a narrow union for keys
export type LoggingModeKey = 'normal' | 'contest';

export type LoggingModeMap = Record<LoggingModeKey, string>;

// Central list of modes
export const loggingModes: LoggingModeMap = {
    normal: 'Normal',
    contest: 'Contest',
};

// Central default key and value
export const DEFAULT_LOGGING_MODE_KEY: LoggingModeKey = 'normal';
export const DEFAULT_LOGGING_MODE_LABEL = loggingModes[DEFAULT_LOGGING_MODE_KEY];

// Well-known keys for specific modes to avoid string literals in components
export const CONTEST_LOGGING_MODE_KEY: LoggingModeKey = 'contest';
export const NORMAL_LOGGING_MODE_KEY: LoggingModeKey = 'normal';

// Store holds just the key; components can derive label as needed
export const loggingModeStore: Writable<LoggingModeKey> = writable(DEFAULT_LOGGING_MODE_KEY);

// Derived helper: true when the current logging mode is contest.
export const isContestMode: Readable<boolean> = derived(
    loggingModeStore,
    ($mode) => $mode === CONTEST_LOGGING_MODE_KEY
);
