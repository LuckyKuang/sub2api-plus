## Responsibility boundary

`push-cli` owns ordinary branch transport and the single local validation
boundary before a pull request is submitted. `release-cli` owns pull-request
promotion, immutable release tags, release workflow observation, publication
verification, and the follow-up metadata pull request. The release tool may
invoke the stable `push-cli submit-pr` command for finalization, but it does not
import or duplicate the push tool's validation implementation.

## Fast push and validated submission

`push` retains the GitHub identity, repository, branch, clean-worktree, and
exact-ref guards. It rejects the GitHub default branch and pushes only
`HEAD:<current-branch>`. It neither probes a container runtime nor waits for
Actions.

`submit-pr` fetches the current default branch and requires it to be an
ancestor of the candidate head. It records the exact base and head commits,
runs the existing complete matrix inside the platform validation container,
then proves that the worktree, base, and head did not change. Only then does it
push the current branch, publish the `sub2api/local-validation` success status
for the exact head, and create or reuse the branch's pull request.

The pull-request body contains a machine-readable base/head marker. The commit
status binds validation to the head. A later commit has no matching status; a
later default-branch update no longer matches the recorded base. Either change
requires another `submit-pr` run.

## Pull-request promotion

Release promotion receives an explicit pull-request number. It verifies the PR
is open, non-draft, targets the default branch, comes from this repository, and
has exactly one matching validation marker and successful local-validation
status for its current head. It also verifies the recorded base is still the
current remote default-branch commit.

The tool requires repository auto-merge, merge-commit mode, strict current-base
enforcement, the complete repository CI/security/local-validation context set,
and a protected default branch before enabling auto-merge. It never uses an
administrator bypass. GitHub remains the authority for required checks and
merge policy. After merge, the tool resolves the actual merge commit and waits
for every push-triggered Actions run at that exact default-branch SHA.

## Release metadata and tags

The application matrix is not repeated during `release-cli validate` or
`tag`. Those actions run the focused release metadata, notes, documentation,
tag-absence, and merge-commit checks. Tag creation targets the promoted PR's
actual merge commit and preserves the validated release notes verbatim.

`publish` only transfers the exact annotated tag. `monitor` resolves that
canonical remote tag and only observes its tag-triggered Release workflow while
retaining the manual protected-environment approval boundary. `verify` requires
the same remote tag and a successfully completed workflow, then checks the
immutable release assets.

## Finalization

Finalization requires a verified published release and a clean worktree. It
fetches `origin/main`, creates a deterministic release-finalization branch from
that remote commit, changes exactly one `UPSTREAM.md` status from `planned` to
`published`, validates the result, and creates one commit. It then invokes
`push-cli submit-pr`; it never pushes or merges `main` directly.

Retries are fail-closed. Existing local branches, remote branches, pull
requests, tags, or Releases are reused only when their exact recorded identity
matches the requested operation. Nothing is reset, force-pushed, retagged, or
overwritten.
