## 1. Push boundary

- [x] 1.1 Split fast ordinary push from platform-container validation.
- [x] 1.2 Add default-branch rejection and exact base/head capture.
- [x] 1.3 Add validated pull-request submission, commit status, and PR reuse.
- [x] 1.4 Cover fast push, fail-closed branch policy, validation races, status,
  and PR behavior with unit tests.

## 2. Release promotion

- [x] 2.1 Add explicit PR inspection and protected auto-merge promotion.
- [x] 2.2 Wait for Actions on the exact merged default-branch commit.
- [x] 2.3 Replace duplicate application preflight with focused release metadata
  and exact-commit tag creation.
- [x] 2.4 Separate publish, monitor, and verify action effects.
- [x] 2.5 Create deterministic finalize branches and submit the follow-up PR.
- [x] 2.6 Add dedicated release-cli unit tests for every state transition and
  fail-closed condition.

## 3. Policy and documentation

- [x] 3.1 Update repository branch/release rules and contributor workflow.
- [x] 3.2 Update both CLI skills, references, and the canonical release guide.
- [x] 3.3 Document required external GitHub ruleset and auto-merge settings.

## 4. Verification

- [x] 4.1 Run focused CLI and release-policy tests inside Apple Containers.
- [x] 4.2 Run strict OpenSpec validation.
- [x] 4.3 Run the repository validation gate in Apple Containers and record any
  external GitHub configuration limitation.
