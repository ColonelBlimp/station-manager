---
apply: always
---

# General Code Style Guidelines

## Naming
- Use clear, descriptive names for variables, functions, and classes which are also self-documenting
- Avoid single-letter names except for loop indices
- Follow consistent naming conventions throughout the project
- For Go naming, keep variable names short but meaningful when the scope is small (e.g., r for reader)
- Avoid generic names like 'data', 'item', or 'value' unless the context makes their purpose undeniable

## Style
- Keep line length reasonable (e.g., 100–120 characters)
- Use consistent indentation and spacing
- Include comments for complex logic or important decisions
- In Go, follow standard gofmt and goimports conventions

## Structure
- Keep functions short and focused on a single responsibility
- Avoid deep nesting and long parameter lists
- Group related code logically
- Use snippet for reusable UI chunks within components instead of multiple small files where logic is tightly coupled

## Best Practices
- Prefer simple, clean, maintainable solutions over clever or complex ones.
- Optimize for performance only after a bottleneck is identified
- Prioritize code that another developer can understand in 30 seconds
- Readability and maintainability are primary concerns
- Avoid duplicate code
- Prefer composition to inheritance
- Avoid swallowing errors; either log them with context or return them up the stack
- Prefer functional programming over OOP
- Prefer Svelte 5 runes ($state, $derived, $props) over Svelte 4 syntax

## Error Handling
- Handle errors explicitly; avoid silent failures
- Use meaningful error messages
- Log errors with sufficient context for debugging

## Testing
- ALWAYS include unit tests for new logic, utility functions, and complex components
- For Go: Place tests in `_test.go` files in the same package; use standard `testing` package or `stretchr/testify` if available
- For TypeScript/Svelte: Use Vitest for unit/component tests and Playwright for E2E tests
- Aim for high coverage of edge cases and error paths, not just the "happy path"
- When refactoring, ensure existing tests pass before and after changes

## Documentation
- Write doc comments for public functions and modules
- Keep documentation up to date with code changes
- Use comments to explain *why*, not *what*
- Keep comments up-to-date with code changes
- Use TODO/FIXME tags for pending work: `// TODO: optimize this loop`

## Tools
- Follow project-specific tooling or linters
- Use version control best practices (e.g., atomic commits, meaningful messages)

## Dependencies
- Minimize external dependencies
- Document why a dependency is needed
- Keep dependencies up-to-date
