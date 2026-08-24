# Verification

## Focused policy and contract checks

All commands ran from the repository root on branch
`optimize-release-validation-flow` at HEAD
`0f5ed15783deb00c01c955830c508595e9e8950f` with the same implementation
worktree used by the full-matrix comparison.

- `python3 skills/compress-cli/scripts/compress_cli.py check AGENTS.md`: passed.
- `python3 skills/compress-cli/tests/test_compress_cli.py`: 14 passed.
- `python3 tools/test_release_policy.py`: 27 passed.
- `python3 tools/test_release_finalization.py`: 8 passed.
- `python3 skills/push-cli/tests/test_push_cli.py`: 42 passed.
- `python3 skills/release-cli/tests/test_release_cli.py`: 28 passed.
- Python bytecode compilation for all changed CLI and policy scripts: passed.
- Repository-pinned `rhysd/actionlint:1.7.12` digest: passed.
- `openspec validate optimize-release-validation-flow --strict
  --no-interactive`: passed with the official OpenSpec CLI.
- `git diff --check`: passed.

The deterministic validator also reproduced the historical finalization from
base `bfa1220152a309ec94a5fed52f02fbceccc27055` to PR head
`d517ed7aa11e3a966b979456f90dc987651959d7` for
`v0.1.178+custom.003`. The corresponding two-parent main merge
`0f5ed15783deb00c01c955830c508595e9e8950f` passed the independent main-topology
validation as well.

## Complete platform-container matrix

Both modes ran through the push-cli validation launcher in the same WSL Debian
Docker runtime, with the same validation image, four-CPU/eight-GB limit, HEAD,
and worktree bytes. Both completed every Go, frontend, CLI, policy, deployment,
migration, build, and audit command successfully. Frontend results were 245
test files and 1,748 tests passed in both modes.

| Mode | Container wall clock | Result |
| --- | ---: | --- |
| Bounded parallel lanes | 433.6s | Passed |
| Serial diagnostic mode | 722.7s | Passed |

Parallel scheduling reduced measured wall clock by 289.1 seconds, or about
40.0 percent. The parallel run executed first; the serial run therefore had
warmer caches and is a favorable serial baseline rather than an inflated one.
The longest parallel lanes were frontend at 433.6s, backend tests at 197.4s,
and backend lint/policy at 154.2s.
