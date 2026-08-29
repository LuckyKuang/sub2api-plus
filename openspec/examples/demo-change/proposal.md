# Add example health summary

## Problem

Operators need a concise health summary without inspecting multiple resources.

## Proposal

- Add a read-only health summary containing service status and update time.
- Keep existing detailed health resources unchanged.

## Non-goals

- Changing health-check scheduling or deployment behavior.

## Impact

- A read-only endpoint, its response model, documentation, and tests change.
