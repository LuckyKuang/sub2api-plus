# Push CLI Reference

## Action Contract

| Action | Local matrix | GitHub mutation | Result |
| --- | --- | --- | --- |
| `push` | None | Exact current-branch push | Fast intermediate branch publication. |
| `submit-pr` | Full platform-container matrix by default; deterministic published-release gate only for release-cli finalization | Exact push, typed commit status, PR create/update | Final candidate with exact typed proof. |
| `check` | Complete platform-container matrix | None | Read-only validation result. |
| `ensure` | Image preparation only | None | Runtime and validation image ready. |
| `watch` | None | None | Watches branch push runs at current HEAD. |

All actions run the GitHub CLI repository gate. `push` and `submit-pr` also
resolve and reject the default branch and require a clean worktree with no
unfinished Git operation.

## Fast Push

The only branch transfer is:

    git push origin HEAD:<current-branch>

Ordinary push returns after Git accepts the ref. It does not run the local
matrix and does not wait for Actions. Use `watch` when remote observation is
needed. Use `submit-pr`, never ordinary push, for the final PR candidate.

## Validated Submission

The default `full` `submit-pr` performs this fail-closed sequence:

1. Fetch `refs/heads/<default>` into `refs/remotes/origin/<default>` without
   fetching tags.
2. Require that exact base commit to be an ancestor of HEAD.
3. Record the 40-character base and head SHAs.
4. Probe and launch the required platform validation container.
5. Run the complete matrix and the runtime-specific final gate.
6. Require a clean worktree and unchanged HEAD.
7. Refetch the default branch and require the same base SHA.
8. Push exactly `HEAD:<current-branch>`.
9. Publish commit status context `sub2api/local-validation` for the head.
10. Create or update one PR whose base/head marker matches those SHAs.

The full marker format is implementation-owned and must occur exactly once:

    <!-- sub2api-submit-pr: {"base":"<sha>","head":"<sha>","profile":"full"} -->

For post-publication metadata, release-cli invokes:

    python3 skills/push-cli/scripts/push_cli.py submit-pr \
      --profile release-finalization --tag vX.Y.Z+custom.NNN

That proof additionally includes the exact tag. Before any mutation, push-cli
requires the deterministic branch, regenerates its complete expected tree from
the recorded base, and verifies the published Release and immutable assets. No
other caller or branch may use this profile.

## In-Container Matrix

The full matrix includes every existing command and runs three bounded lanes:

- Go module tidiness, unit tests, integration tests, and golangci-lint.
- Compress CLI, push CLI, and release CLI self-tests.
- Frozen pnpm install, lint, typecheck, Vitest, production build, and production
  audit exception policy.
- Release policy, release metadata, README synchronization, Codex outbound
  identity, and migration checks against the validated default-branch base.
- Installer syntax, Docker deployment security/resources, Caddy cache policy,
  and the Apple Container lifecycle fixture.

The backend-test lane keeps module tidiness before unit and integration tests.
The backend-lint/policy lane serializes lint and repository policy commands.
The frontend lane keeps install before lint, typecheck, full Vitest, build, and
audit. Default parallel execution limits Vitest to two workers within the
four-CPU container; `--serial` uses four. Every command and lane reports elapsed
monotonic time. A failure stops new steps and joins already-running commands;
neither scheduling mode can publish a partial proof.

Apple Containers reports Docker Compose parsing as not applicable. WSL2 Docker
and Linux Docker must parse `deploy/docker-compose.dev.yml` successfully.

## Recovery

- If ordinary push succeeds but remote Actions fail, fix on the branch and push
  again; no local proof was issued.
- If `submit-pr` validation fails, no push/status/PR mutation occurs.
- If the default branch changes during validation, rerun `submit-pr` after
  updating the branch.
- If push succeeds but status or PR creation fails, rerun `submit-pr`; the
  complete matrix runs again and the exact matching PR may be reused.
- If the PR head changes later, its old status and marker cannot authorize
  release promotion.
