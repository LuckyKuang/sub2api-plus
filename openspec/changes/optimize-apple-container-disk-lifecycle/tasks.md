## 1. Deployment lifecycle

- [x] 1.1 Keep normal application redeployment as delete-before-create while
  preserving every persistent mount.
- [x] 1.2 Add application-only, health-gated image upgrade with one stable
  rollback reference and optional previous-image deletion.
- [x] 1.3 Add disk usage reporting and explicit dangling-image cleanup.
- [x] 1.4 Add verified cleanup for owned named volumes replaced by bind mounts.

## 2. Tests and documentation

- [x] 2.1 Extend the fake Apple CLI with image, mount, disk, and command state.
- [x] 2.2 Cover application-only redeployment, failed pull rollback retention,
  successful upgrade pruning, global prune opt-in, and fail-closed volume
  cleanup.
- [x] 2.3 Document commands as individually copyable operations and explain
  writable-layer, persistent-data, rollback, and global cleanup boundaries.

## 3. Verification

- [x] 3.1 Run Bash syntax, lifecycle, and whitespace checks.
- [x] 3.2 Validate image tag creation and deletion with Apple Container CLI
  1.2.0 without changing the running stack.
- [x] 3.3 Run strict OpenSpec and repository deployment/release checks.
