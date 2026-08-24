## MODIFIED Requirements

### Requirement: Pull-request submission must bind validation to exact commits

The push tool SHALL issue an explicit validation profile for pull-request
submission. The `full` profile MUST remain the default and MUST run the complete
platform-container matrix for every ordinary and release-candidate pull
request. A `release-finalization` profile MAY run focused validation only when
the exact custom tag is already published and the complete head tree is the
deterministic finalization result for the recorded default-branch base. The PR
marker and commit status MUST bind the profile, exact base/head SHAs, and tag
when applicable.

#### Scenario: Ordinary candidate is submitted successfully

- **WHEN** an ordinary candidate contains the latest default-branch commit and
  the complete local matrix succeeds
- **THEN** the proof profile SHALL be `full`
- **THEN** the tool SHALL recheck the unchanged base and head, push the exact
  head, publish the success status, and create or reuse one pull request

#### Scenario: Published metadata is submitted successfully

- **WHEN** release-cli requests `release-finalization` for a verified published
  tag on its deterministic branch
- **THEN** the tool SHALL regenerate the complete expected tree from the exact
  base and compare it with the head
- **THEN** it SHALL bind the successful focused proof to that base, head,
  profile, and tag without running the complete application matrix

#### Scenario: Profile, commits, or deterministic output changes

- **WHEN** the proof profile, tag, head, base, mapping transition, generated
  documentation, or worktree differs from the validated values
- **THEN** release promotion MUST reject the pull request
- **THEN** no focused proof may authorize the changed candidate

### Requirement: Release promotion must use protected GitHub auto-merge

The release tool SHALL promote only an explicit, open, non-draft pull request
whose typed validation proof matches its current head and base. A release
candidate with notes MUST carry the `full` profile. A no-notes finalization MUST
carry the matching `release-finalization` profile and MUST independently pass
the deterministic published-release validator. The tool MUST continue to
require default-branch protection, repository auto-merge, merge-commit mode, a
strict current-base policy, the complete required context set, and successful
Actions without administrator bypass.

#### Scenario: Protected full-profile pull request is ready

- **WHEN** the full proof matches and every protected merge condition succeeds
- **THEN** the release tool SHALL enable native merge-commit auto-merge
- **THEN** it SHALL wait for push-triggered Actions at the exact merge commit

#### Scenario: Protected finalization pull request is ready

- **WHEN** the focused proof matches the requested published tag and regenerated
  tree and every protected merge condition succeeds
- **THEN** the release tool MAY promote it through the same native auto-merge
- **THEN** the merged-main Actions MUST revalidate the focused change at the
  actual merge commit

### Requirement: Post-publication metadata must return through a pull request

The release tool SHALL finalize a verified publication on a deterministic
branch created from the latest `origin/main`. It MUST generate only the matching
`planned` to `published` transition and deterministic release-document updates,
commit that result, and submit it through the explicit
`release-finalization` proof profile. It MUST NOT push or merge `main` directly.

#### Scenario: Published release is finalized

- **WHEN** the Release workflow and immutable assets are verified
- **THEN** exactly one `planned` mapping SHALL become `published`
- **THEN** every other changed byte SHALL be reproducible by the release
  documentation generator from the recorded base
- **THEN** the follow-up SHALL be submitted as a protected pull request
