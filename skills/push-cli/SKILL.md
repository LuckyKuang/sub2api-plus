---
name: push-cli
description: >-
  Safely push Sub2API Plus working branches and submit the final locally
  validated pull request. Use when the user asks to push code, publish the
  current branch, run the repository validation matrix, create or update a
  pull request, or verify branch CI. Ordinary push is fast and never runs the
  local matrix. submit-pr is the only final promotion boundary: it requires
  the latest default-branch base, runs every check inside the platform
  validation container, binds the result to exact base/head SHAs, pushes,
  publishes the local-validation commit status, and creates or reuses the pull
  request. Never push the repository default branch.
---

# Push CLI

Run commands from the repository root:

    python3 skills/push-cli/scripts/push_cli.py push
    python3 skills/push-cli/scripts/push_cli.py submit-pr
    python3 skills/push-cli/scripts/push_cli.py check
    python3 skills/push-cli/scripts/push_cli.py ensure
    python3 skills/push-cli/scripts/push_cli.py watch

`push` performs an authenticated exact-ref push of the clean current working
branch. It does not probe a container runtime, run local tests, create a pull
request, or wait for Actions. It rejects the GitHub default branch, detached
HEAD, unfinished Git operations, an unexpected remote, and every force/tag/all
push form.

`submit-pr` is the final promotion action. It fetches the current GitHub default
branch, requires that commit to be contained by the candidate branch, records
the exact base and head, and runs the complete local matrix inside the platform
validation container. After the matrix it refetches the default branch and
requires the worktree, base, and head to be unchanged. It then pushes the exact
head, publishes `sub2api/local-validation=success`, and creates or reuses one
pull request to the default branch.

`check` runs the same local matrix without pushing or creating a PR. `ensure`
only prepares the platform runtime and validation image. `watch` observes
push-triggered Actions for the current branch and SHA.

## Mandatory GitHub CLI Gate

Every action requires an installed and authenticated GitHub CLI. Resolve the
origin repository exactly as `LuckyKuang/sub2api-plus`, verify repository
access and push permission, and resolve the default branch from GitHub. Never
run `gh auth login` automatically or fall back to another credential or HTTP
client.

Before an authorized push, configure Git transport with `gh auth setup-git`.
Git transfers only `HEAD:<current-branch>`. Never use `--force`, `--all`,
`--tags`, or another local ref.

## Validation Runtime

Only `check`, `submit-pr`, and `ensure` access the validation runtime.

- macOS: Apple Containers only; no Docker, Colima, or host-toolchain fallback.
- Windows: Docker inside a running WSL2 Debian or Ubuntu distribution only.
- Linux: directly reachable Docker Engine and Compose plugin.

Every Go, frontend, Python policy, installer, and lifecycle check runs inside
`deploy/Dockerfile.validation`. Host processes may only perform GitHub/Git
gates, runtime probes, image management, Compose parsing, and container launch.

## Pull-Request Proof

The PR body contains one machine-readable base/head marker and its head commit
carries the `sub2api/local-validation` status. A later branch commit has no
matching status. A later default-branch update no longer matches the marker.
Either condition requires another `submit-pr`; never edit the marker or status
manually.

`submit-pr` creates a ready PR by default. `--title` and `--body-file` may
provide reviewed PR text. If exactly one open PR already exists for the same
head/base, update only its validation marker. Refuse ambiguous PRs or mismatched
head/base SHAs.

## Safety

- Never push the repository default branch, a dirty worktree, detached HEAD,
  or an unfinished merge/rebase/cherry-pick/revert.
- Never imply that ordinary `push` performed local validation.
- Never publish a success status until the post-matrix base/head/worktree
  recheck and exact branch push both succeed.
- Never create a PR for a stale default-branch base.
- Never run the matrix on the host or silently downgrade tool versions.
- Treat a local validation failure as a hard stop before push or PR mutation.

Read `references/push-cli.md` for the exact matrix, proof format, and recovery
rules. Use `scripts/push_cli.py` for every push or submission action.
