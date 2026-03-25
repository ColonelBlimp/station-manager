# Focus Context API Documentation

## Overview

The Focus Context module provides a type-safe, reactive approach to managing focus across Svelte 5 components using Svelte's context API and `$state` runes.

## Problem

Traditional focus management in web applications often involves:

```javascript
// ❌ Bad: Direct DOM queries
document.getElementById('callsign-input')?.focus();
```

This approach has several issues:
- **Fragile**: IDs can change, typos cause silent failures
- **Not type-safe**: No TypeScript support for valid element names
- **Hard to test**: Requires DOM mocking
- **Tightly coupled**: Components need to know implementation details

## Solution

Focus Context provides:

```typescript
// ✅ Good: Type-safe, reactive focus management
const focusContext = getFocusContext();
await focusContext.focus('callsignInput');
```

Benefits:
- **Type-safe**: TypeScript ensures only valid ref names are used
- **Reactive**: Svelte 5 `$state` keeps refs synchronized
- **Testable**: Easy to mock the context in tests
- **Decoupled**: Components only know about logical element names

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Root Layout                              │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                    setFocusContext()                       │  │
│  │  Creates FocusRefsClass (with $state) + FocusContext       │  │
│  │  Stores in Svelte context with Symbol key                  │  │
│  └───────────────────────────────────────────────────────────┘  │
│                              │                                   │
│                              ▼                                   │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │                    Child Components                        │  │
│  │                                                            │  │
│  │  Component A:                    Component B:              │  │
│  │  ┌─────────────────────┐        ┌────────────────────┐    │  │
│  │  │ getFocusContext()   │        │ getFocusContext()  │    │  │
│  │  │                     │        │                    │    │  │
│  │  │ <input bind:this=   │        │ focus('callsign')  │    │  │
│  │  │  {refs.callsign}>   │        │                    │    │  │
│  │  └─────────────────────┘        └────────────────────┘    │  │
│  └───────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

## API Reference

### Types

#### `FocusRefs`

Interface defining all focusable element references:

```typescript
interface FocusRefs {
    // QSO Panel inputs
    callsignInput: HTMLInputElement | null;
    srxRcvdInput: HTMLInputElement | null;

    // Station Panel inputs
    operatorCallsignInput: HTMLInputElement | null;

    // Info Panel inputs
    fwdSessionEmailInput: HTMLInputElement | null;

    // Modal inputs
    editCallsignInput: HTMLInputElement | null;
}
```

#### `FocusContext`

Interface for the focus context object:

```typescript
interface FocusContext {
    /** All focusable element references */
    refs: FocusRefs;
    
    /**
     * Focus an element by ref name
     * @param refName - Key from FocusRefs
     * @param select - Whether to select content (default: false)
     */
    focus(refName: keyof FocusRefs, select?: boolean): Promise<void>;
}
```

### Functions

#### `setFocusContext(): FocusContext`

Creates and registers the focus context. **Must be called in a root/layout component.**

```typescript
// +layout.svelte
<script lang="ts">
    import { setFocusContext } from '@station-manager/shared-utils/svelte';
    
    // Call during component initialization
    const focusContext = setFocusContext();
</script>

<slot />
```

#### `getFocusContext(): FocusContext`

Retrieves the focus context. **Must be called in a descendant component.**

```typescript
<script lang="ts">
    import { getFocusContext } from '@station-manager/shared-utils/svelte';
    
    const focusContext = getFocusContext();
</script>
```

**Throws** `Error` if called outside of a context tree where `setFocusContext()` was called.

## Usage Patterns

### Pattern 1: Registering Focusable Elements

```svelte
<!-- QSOPanel.svelte -->
<script lang="ts">
    import { getFocusContext } from '@station-manager/shared-utils/svelte';
    
    const { refs } = getFocusContext();
</script>

<input
    type="text"
    bind:this={refs.callsignInput}
    placeholder="Callsign"
/>

<input
    type="text"
    bind:this={refs.srxRcvdInput}
    placeholder="Serial Received"
/>
```

### Pattern 2: Triggering Focus

```svelte
<!-- FormActions.svelte -->
<script lang="ts">
    import { getFocusContext } from '@station-manager/shared-utils/svelte';
    
    const focusContext = getFocusContext();
    
    async function handleClear() {
        clearForm();
        // Focus callsign input for next entry
        await focusContext.focus('callsignInput');
    }
    
    async function handleTabToNext() {
        // Focus and select next field
        await focusContext.focus('srxRcvdInput', true);
    }
</script>

<button onclick={handleClear}>Clear</button>
<button onclick={handleTabToNext}>Next Field</button>
```

### Pattern 3: Focus After State Changes

```svelte
<script lang="ts">
    import { getFocusContext } from '@station-manager/shared-utils/svelte';
    
    const focusContext = getFocusContext();
    
    let showModal = $state(false);
    
    async function openEditModal() {
        showModal = true;
        // focus() uses tick() internally, so this works
        // even though the modal just appeared
        await focusContext.focus('editCallsignInput');
    }
</script>

{#if showModal}
    <Modal>
        <input bind:this={focusContext.refs.editCallsignInput} />
    </Modal>
{/if}
```

### Pattern 4: Conditional Focus

```svelte
<script lang="ts">
    import { getFocusContext } from '@station-manager/shared-utils/svelte';
    
    const { refs, focus } = getFocusContext();
    
    async function focusNextAvailable() {
        // Focus the first available input
        if (refs.srxRcvdInput) {
            await focus('srxRcvdInput');
        } else {
            await focus('callsignInput');
        }
    }
</script>
```

## Extending Focus Refs

To add new focusable elements to the system:

### Step 1: Update `FocusRefs` Interface

```typescript
// In shared-utils/src/lib/svelte/focus-context.svelte.ts

export interface FocusRefs {
    // ...existing refs...
    
    // Your new ref
    searchInput: HTMLInputElement | null;
}
```

### Step 2: Update `FocusRefsClass`

```typescript
class FocusRefsClass implements FocusRefs {
    // ...existing refs...
    
    // Your new ref with $state
    searchInput: HTMLInputElement | null = $state(null);
}
```

### Step 3: Rebuild shared-utils

```bash
cd shared-utils
npm run build
```

### Step 4: Use in Components

```svelte
<input bind:this={focusContext.refs.searchInput} />
```

## Testing

### Unit Testing Components

```typescript
import { describe, it, vi } from 'vitest';

// Mock the module
vi.mock('svelte', () => ({
    getContext: vi.fn(),
    setContext: vi.fn(),
    tick: vi.fn().mockResolvedValue(undefined),
}));

import { setFocusContext, getFocusContext } from '@station-manager/shared-utils/svelte';
import { getContext, setContext } from 'svelte';

describe('Component with focus', () => {
    it('should focus on mount', async () => {
        const mockFocus = vi.fn();
        const mockContext = {
            refs: { callsignInput: { focus: mockFocus } },
            focus: vi.fn(),
        };
        
        vi.mocked(getContext).mockReturnValue(mockContext);
        
        // Test your component
    });
});
```

### Integration Testing

For integration tests with actual Svelte components, use `@testing-library/svelte`:

```typescript
import { render } from '@testing-library/svelte';
import Layout from './+layout.svelte';

describe('Focus integration', () => {
    it('should focus element when action triggered', async () => {
        const { getByRole, getByText } = render(Layout);
        
        const input = getByRole('textbox');
        const button = getByText('Focus');
        
        await button.click();
        
        expect(document.activeElement).toBe(input);
    });
});
```

## Troubleshooting

### Error: "FocusContext not found"

**Cause**: `getFocusContext()` was called in a component that is not a descendant of where `setFocusContext()` was called.

**Solution**: Ensure `setFocusContext()` is called in your root layout (`+layout.svelte`), and the component calling `getFocusContext()` is rendered within that layout.

### Focus not working after state change

**Cause**: The element may not be in the DOM yet when `focus()` is called.

**Solution**: The `focus()` method already calls `tick()` internally. If issues persist, ensure the element is rendered before calling focus:

```typescript
showElement = true;
await tick(); // Extra tick if needed
await focusContext.focus('elementRef');
```

### TypeScript error: "not assignable to keyof FocusRefs"

**Cause**: The ref name doesn't exist in `FocusRefs` interface.

**Solution**: Either use an existing ref name or add your new ref to both `FocusRefs` interface and `FocusRefsClass` in the source.

## Migration Guide

### From document.getElementById

Before:
```typescript
document.getElementById('callsign-input')?.focus();
```

After:
```typescript
const { focus } = getFocusContext();
await focus('callsignInput');
```

### From direct element refs

Before:
```svelte
<script>
    let callsignInput;
    
    function focusCallsign() {
        callsignInput?.focus();
    }
</script>

<input bind:this={callsignInput} />
```

After:
```svelte
<script>
    import { getFocusContext } from '@station-manager/shared-utils/svelte';
    
    const { refs, focus } = getFocusContext();
    
    async function focusCallsign() {
        await focus('callsignInput');
    }
</script>

<input bind:this={refs.callsignInput} />
```

The key benefit is that other components can now trigger focus without needing a reference to the input.

