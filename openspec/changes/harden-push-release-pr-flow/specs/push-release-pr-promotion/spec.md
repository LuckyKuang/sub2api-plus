## ADDED Requirements

### Requirement: Ordinary pushes must be fast and default-branch safe

The push tool SHALL push a clean current branch without running the local
application validation matrix. It MUST resolve and reject the GitHub repository
default branch, detached HEAD, force push, tag push, and an unreviewed remote.

#### Scenario: Developer pushes an intermediate branch commit

- **WHEN** the authenticated developer runs ordinary push from a clean
  non-default branch
- **THEN** the exact current head SHALL be pushed to the same remote branch
- **THEN** no container runtime or local test matrix SHALL be started
- **THEN** the command SHALL return without waiting for GitHub Actions

#### Scenario: Developer attempts to push main

- **WHEN** the current branch is the GitHub repository default branch
- **THEN** ordinary push and pull-request submission MUST fail before any Git
  mutation

### Requirement: Pull-request submission must bind validation to exact commits

The push tool SHALL run the complete platform-container validation matrix only
for explicit pull-request submission. The resulting pull request MUST identify
the exact validated head and default-branch base, and the head MUST carry a
successful `sub2api/local-validation` commit status.

#### Scenario: Candidate is submitted successfully

- **WHEN** the candidate contains the latest default-branch commit, the
  worktree is clean, and the complete local matrix succeeds
- **THEN** the tool SHALL recheck the unchanged base and head
- **THEN** it SHALL push the exact head, publish the success status, and create
  or reuse one pull request to the default branch

#### Scenario: Candidate changes after validation

- **WHEN** the head or default-branch base differs from the values validated by
  `submit-pr`
- **THEN** release promotion MUST reject that pull request
- **THEN** the candidate MUST be resubmitted through the local validation gate

### Requirement: Release promotion must use protected GitHub auto-merge

The release tool SHALL promote only an explicit, open, non-draft pull request
whose validation proof matches its current head and base. It MUST require
default-branch protection, repository auto-merge, merge-commit mode, a strict
current-base policy, the complete repository CI/security/local-validation
context set, and successful required Actions. It MUST NOT use an administrator
bypass.

#### Scenario: Protected pull request is ready

- **WHEN** the validation proof matches, GitHub required checks succeed, and
  every protected merge condition is satisfied
- **THEN** the release tool SHALL enable GitHub auto-merge using the repository
  merge-commit method
- **THEN** it SHALL wait for the pull request to reach the merged state
- **THEN** it SHALL wait for push-triggered Actions at the exact merge commit

#### Scenario: Repository protection is absent

- **WHEN** auto-merge is disabled or the default branch has no active
  protection/ruleset
- **THEN** the release tool MUST fail without directly merging the pull request

### Requirement: Release tags must follow the tested main commit

Release metadata validation SHALL be focused and SHALL NOT rerun the complete
local application matrix. A new annotated tag MUST target the promoted pull
request's actual merge commit after its default-branch push Actions succeed.
After publication, monitoring, verification, and finalization MUST resolve the
canonical remote annotated tag instead of trusting a possibly stale local tag.

#### Scenario: Merge commit is ready to tag

- **WHEN** the exact merge commit is contained by `origin/main`, its push
  Actions succeeded, release metadata is planned and consistent, and the tag is
  absent locally and remotely
- **THEN** the release tool MAY create one annotated tag at that commit
- **THEN** tag publication SHALL remain a separate explicit action

### Requirement: Post-publication metadata must return through a pull request

The release tool SHALL finalize a verified publication on a deterministic
branch created from the latest `origin/main`. It MUST modify only the matching
`UPSTREAM.md` status, commit that change, and submit it through the same local
validation and pull-request boundary.

#### Scenario: Published release is finalized

- **WHEN** the Release workflow and immutable assets are verified
- **THEN** exactly one `planned` mapping SHALL become `published`
- **THEN** no commit or branch push SHALL target `main` directly
- **THEN** the follow-up SHALL be submitted as a pull request
