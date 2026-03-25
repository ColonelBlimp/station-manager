# @station-manager/shared-utils

Shared utility functions and Svelte utilities for Station Manager applications.

## Build

```bash
npm run build
```

## Installation

```bash
npm install @station-manager/shared-utils
```

## Usage

### Core Utilities

Import core utilities from the main package:

```typescript
import { createErrorHandler, getErrorMessage, formatFrequency } from '@station-manager/shared-utils';
```

### Svelte Utilities

Import Svelte-specific utilities from the `/svelte` subpath:

```typescript
import { setFocusContext, getFocusContext } from '@station-manager/shared-utils/svelte';
```

---

## Svelte Utilities

### Focus Context

A Svelte context-based system for managing focus across components, replacing direct DOM manipulation with type-safe, reactive element references.

#### Why Use Focus Context?

- **Type-safe**: All focusable elements are defined in a typed interface
- **Reactive**: Uses Svelte 5's `$state` for reactive element references
- **Decoupled**: Components don't need to know DOM IDs or query the document
- **Testable**: Focus operations can be easily mocked and tested

#### Quick Start

##### 1. Initialize in Root Layout

Call `setFocusContext()` in your root layout (`+layout.svelte` or `App.svelte`):

```svelte
<!-- src/routes/+layout.svelte -->
<script lang="ts">
  import { setFocusContext } from '@station-manager/shared-utils/svelte';

  // Initialize the focus context at the root of your app
  setFocusContext();
</script>

<slot />
```

##### 2. Register Focusable Elements

In components with focusable elements, bind them to the context's refs:

```svelte
<!-- src/lib/components/QSOPanel.svelte -->
<script lang="ts">
  import { getFocusContext } from '@station-manager/shared-utils/svelte';

  const focusContext = getFocusContext();
</script>

<!-- Bind the input to the context's refs -->
<input
  type="text"
  bind:this={focusContext.refs.callsignInput}
  placeholder="Enter callsign"
/>
```

##### 3. Focus Elements from Anywhere

Any component in the tree can focus registered elements:

```svelte
<!-- src/lib/components/QSOActions.svelte -->
<script lang="ts">
  import { getFocusContext } from '@station-manager/shared-utils/svelte';

  const focusContext = getFocusContext();

  async function handleClear() {
    // Clear form logic...
    
    // Focus and select the callsign input
    await focusContext.focus('callsignInput', true);
  }
</script>

<button onclick={handleClear}>Clear QSO</button>
```

#### API Reference

##### `setFocusContext(): FocusContext`

Creates and sets the focus context in Svelte's context. Must be called in a parent/root component during initialization.

**Returns:** The created `FocusContext` instance

**Example:**
```typescript
const focusContext = setFocusContext();
```

##### `getFocusContext(): FocusContext`

Gets the focus context from Svelte's context. Must be called in a descendant of the component where `setFocusContext()` was called.

**Returns:** The `FocusContext` instance

**Throws:** Error if called outside of a component tree that has FocusContext

**Example:**
```typescript
const focusContext = getFocusContext();
```

##### `FocusContext.refs`

Object containing all focusable element references. Bind elements using Svelte's `bind:this` directive.

**Type:** `FocusRefs`

##### `FocusContext.focus(refName, select?): Promise<void>`

Focus an element by its ref name, optionally selecting its content.

**Parameters:**
- `refName` (`keyof FocusRefs`): The name of the ref to focus
- `select` (`boolean`, optional): Whether to select the input content (default: `false`)

**Returns:** Promise that resolves when focus is complete

**Example:**
```typescript
// Simple focus
await focusContext.focus('callsignInput');

// Focus and select all text
await focusContext.focus('callsignInput', true);
```

#### Available Focus Refs

The following refs are currently defined in `FocusRefs`:

| Ref Name | Type | Description |
|----------|------|-------------|
| `callsignInput` | `HTMLInputElement` | Primary callsign input in QSO panel |
| `srxRcvdInput` | `HTMLInputElement` | Serial number received input |
| `operatorCallsignInput` | `HTMLInputElement` | Operator's callsign in station settings |
| `fwdSessionEmailInput` | `HTMLInputElement` | Email input for forwarding session |
| `editCallsignInput` | `HTMLInputElement` | Callsign input in session edit modal |

#### Extending Focus Refs

To add new focusable elements, modify the `FocusRefs` interface and `FocusRefsClass` in `focus-context.svelte.ts`:

```typescript
// 1. Add to FocusRefs interface:
export interface FocusRefs {
  // ...existing refs...
  myNewInput: HTMLInputElement | null;
}

// 2. Add to FocusRefsClass:
class FocusRefsClass implements FocusRefs {
  // ...existing refs...
  myNewInput: HTMLInputElement | null = $state(null);
}
```

Then use in your component:

```svelte
<input bind:this={focusContext.refs.myNewInput} />
```

And focus from anywhere:

```typescript
await focusContext.focus('myNewInput');
```

#### Common Patterns

##### Focus After Form Submission

```svelte
<script lang="ts">
  import { getFocusContext } from '@station-manager/shared-utils/svelte';

  const focusContext = getFocusContext();

  async function handleSubmit() {
    await saveQSO();
    clearForm();
    
    // Return focus to callsign input for next QSO
    await focusContext.focus('callsignInput');
  }
</script>
```

##### Focus When Modal Opens

```svelte
<script lang="ts">
  import { getFocusContext } from '@station-manager/shared-utils/svelte';

  const focusContext = getFocusContext();
  let showModal = $state(false);

  async function openModal() {
    showModal = true;
    // Focus the modal's input after it renders
    await focusContext.focus('editCallsignInput');
  }
</script>
```

##### Conditional Focus Based on Element Availability

```svelte
<script lang="ts">
  import { getFocusContext } from '@station-manager/shared-utils/svelte';

  const focusContext = getFocusContext();

  async function focusNextInput() {
    // The focus method safely handles null refs
    if (focusContext.refs.srxRcvdInput) {
      await focusContext.focus('srxRcvdInput');
    } else {
      await focusContext.focus('callsignInput');
    }
  }
</script>
```

---

## Core Utilities

### Error Handler

Factory for creating error handlers with configurable logging and notification:

```typescript
import { createErrorHandler, getErrorMessage } from '@station-manager/shared-utils';

const handleError = createErrorHandler({
  logger: (msg) => console.error(msg),
  notifier: (msg) => showToast(msg),
});

try {
  await riskyOperation();
} catch (error) {
  handleError(error, 'Failed to complete operation');
}
```

### Frequency Utilities

Format and manipulate radio frequencies:

```typescript
import { formatFrequency } from '@station-manager/shared-utils';

const display = formatFrequency(14074000); // "14.074"
```

### Time/Date Utilities

Common time and date formatting functions for amateur radio logging.

---

## Requirements

- Node.js 18+
- For Svelte utilities: Svelte 5.0+

## License

MIT
