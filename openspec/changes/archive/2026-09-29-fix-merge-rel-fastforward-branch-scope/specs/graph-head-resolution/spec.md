## ADDED Requirements

### Requirement: Merge relationship fast-forward targets the intended branch

When a branch merge applies a relationship `fast_forward`, the previous HEAD it
supersedes SHALL be the HEAD **on the merge target branch** (`targetBranchID`), not
the branch-less main-preferring default. The new relationship version SHALL be
written on the target branch; the `main` branch SHALL NOT be modified when the
target is a named branch. A relationship version write whose explicitly requested
branch disagrees with the resolved prev-head's branch SHALL fail rather than
silently write to a different branch.

#### Scenario: Branch→branch fast-forward writes to the target branch

- **WHEN** the same relationship `canonical_id` exists on `main`, a source branch
  and a target branch, the source and target contents differ, and the source
  branch is merged into the **target branch** (`targetBranchID` non-nil)
- **THEN** the new HEAD version is written on the target branch with the source
  content
- **AND** the `main` HEAD is left unchanged

#### Scenario: Branch→main fast-forward still targets main

- **WHEN** the source branch is merged into `main` (`targetBranchID` nil)
- **THEN** the new HEAD version is written on `main`, as before

#### Scenario: Mismatched requested branch fails loudly

- **WHEN** a relationship version write passes an explicit branch that disagrees
  with the resolved prev-head's branch
- **THEN** the write fails with an error rather than superseding a HEAD on the
  wrong branch
