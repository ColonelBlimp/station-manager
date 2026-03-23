/**
 * Common CSS class patterns extracted from components.
 * Use these constants to maintain consistency across the UI.
 */

// ============================================================================
// Base Input Styles
// ============================================================================

/**
 * Base styles for text inputs.
 * Used by: TextInput, Callsign, Rst, VfoInput
 */
export const inputBase =
    'block w-full rounded-md bg-white px-3 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600';

/**
 * Base styles for text inputs with uppercase transformation.
 * Used by: Callsign, Rst
 */
export const inputBaseUppercase = `uppercase ${inputBase}`;

/**
 * Base styles for date/time picker inputs (slightly less horizontal padding).
 * Used by: DateInput, TimeInput
 */
export const inputDateTimePicker =
    'block w-full rounded-md bg-white px-1.5 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600';

/**
 * Base styles for select dropdown inputs.
 * Used by: Mode
 */
export const selectBase =
    'col-start-1 row-start-1 w-full appearance-none rounded-md bg-white py-1.5 pr-8 pl-3 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600';

/**
 * Base styles for textarea inputs (like Comment).
 * Used by: Comment
 */
export const textareaBase =
    'resize-none w-full rounded-md bg-white px-2 py-1.5 text-base outline-1 -outline-offset-1 outline-gray-300 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600';

/**
 * Compact input style for inline/smaller inputs (like email in header).
 */
export const inputCompact =
    'w-full outline-1 outline-gray-300 rounded-md px-2 py-1 focus:outline-2 focus:-outline-offset-2 focus:outline-indigo-600 text-sm/6';

// ============================================================================
// Label Styles
// ============================================================================

/**
 * Standard label styles for form inputs.
 */
export const labelBase = 'block text-sm/5 font-medium';

/**
 * Label styles with left margin (used in Comment component).
 */
export const labelWithMargin = 'block text-sm/5 font-medium ml-2';

// ============================================================================
// Layout Styles
// ============================================================================

/**
 * Standard wrapper div for inputs (provides top margin).
 */
export const inputWrapper = 'mt-2';

/**
 * Grid wrapper for select components.
 */
export const selectWrapper = 'mt-2 grid grid-cols-1';

/**
 * Relative wrapper for inputs with icons (date/time pickers).
 */
export const inputWrapperRelative = 'relative mt-2';

// ============================================================================
// Validation States
// ============================================================================

/**
 * Invalid input state outline styles.
 */
export const inputInvalid = 'outline-red-600 focus:outline-red-600';

/**
 * Disabled timer input styles (orange highlighting).
 */
export const inputTimerDisabled =
    'disabled:outline-orange-500 disabled:outline-2 disabled:bg-orange-200';

// ============================================================================
// Button Styles
// ============================================================================

/**
 * Base button with cursor styles.
 */
export const buttonCursor = 'cursor-pointer disabled:cursor-not-allowed';

// ============================================================================
// Icon Styles
// ============================================================================

/**
 * Standard icon size and color for input decorations.
 */
export const inputIcon = 'size-5 text-gray-700';

/**
 * Smaller icon for select dropdowns.
 */
export const selectIcon =
    'pointer-events-none col-start-1 row-start-1 mr-2 size-5 self-center justify-self-end text-gray-500 sm:size-4';

// ============================================================================
// Tab/Panel Header Button Styles
// ============================================================================

/**
 * Base styles for tab buttons.
 */
export const tabButtonBase = 'flex gap-x-2';

/**
 * Selected tab button styles.
 */
export const tabButtonSelected = `${tabButtonBase} cursor-default text-indigo-600 font-semibold`;

/**
 * Unselected tab button styles.
 */
export const tabButtonUnselected = `${tabButtonBase} cursor-pointer text-gray-500 font-semibold hover:text-gray-700`;

/**
 * Helper function to get tab button class based on selection state.
 */
export const getTabButtonClass = (isSelected: boolean): string =>
    isSelected ? tabButtonSelected : tabButtonUnselected;

// ============================================================================
// Table/List Styles (Session Panel)
// ============================================================================

export const sessionTable = {
    distance: 'w-[92px]',
    time: 'w-[74px]',
    callsign: 'w-[90px]',
    band: 'w-[50px]',
    freq: 'w-[80px]',
    rst: 'w-[50px]',
    mode: 'w-[52px]',
    country: 'w-[140px] text-nowrap overflow-hidden text-ellipsis pr-1',
    name: 'w-[140px] text-nowrap overflow-hidden text-ellipsis pr-1',
} as const;

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Combines base input styles with optional additional classes.
 */
export const combineInputStyles = (additionalClasses: string = ''): string =>
    additionalClasses ? `${inputBase} ${additionalClasses}` : inputBase;

/**
 * Combines styles with invalid state when applicable.
 */
export const withInvalidState = (baseStyles: string, isInvalid: boolean): string =>
    isInvalid ? `${baseStyles} ${inputInvalid}` : baseStyles;
