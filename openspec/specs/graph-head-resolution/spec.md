# graph-head-resolution Specification

## Purpose
Defines the deterministic rule by which a graph object or relationship lookup resolves
a HEAD row when a `canonical_id` is shared by the main branch and one or more fork
copies. A lookup that carries no branch context SHALL resolve to `main`; a lookup that
carries an explicit branch context SHALL resolve within that branch; an explicit
physical id SHALL always win.

## Requirements

### Requirement: Branch-less canonical lookup resolves to the main HEAD

When a graph object or relationship lookup accepts either a physical id or a
`canonical_id` and no branch context is supplied, and several HEAD rows
(`supersedes_id IS NULL`) share that `canonical_id` because the object was forked to
one or more branches, the resolver SHALL return the `main` HEAD (`branch_id IS NULL`).
The result SHALL NOT depend on the UUID ordering of the matching rows.

#### Scenario: Fork copy sorts first by id

- **WHEN** an object has a main HEAD and a fork-copy HEAD sharing a `canonical_id`, and the fork copy's `id` sorts before the main HEAD's `id`
- **THEN** a canonical-id lookup with no branch context returns the main HEAD

#### Scenario: Main HEAD sorts first by id

- **WHEN** the same fork fixture has the main HEAD's `id` sorting before the fork copy's `id`
- **THEN** a canonical-id lookup with no branch context returns the main HEAD

#### Scenario: BranchID-less Patch targets main

- **WHEN** a `Patch` without a `BranchID` is applied to a forked object's `canonical_id`
- **THEN** a new version is created on the main HEAD and the fork copy is left unchanged

### Requirement: Explicit physical id wins

An explicit physical row id SHALL take precedence over the main-HEAD default, so a
caller can address a specific version or a fork copy directly.

#### Scenario: Fork copy addressed by physical id

- **WHEN** a caller passes the fork copy's physical id, with or without a `BranchID`
- **THEN** the fork copy is the resolved target

#### Scenario: Explicit branch context targets the branch

- **WHEN** a mutation supplies an explicit `BranchID`
- **THEN** it resolves the HEAD within that branch (via the branch-aware canonical resolver), independent of the fork copy's UUID ordering

### Requirement: Relationship resolvers share the object contract

`GetRelationshipByID` and `GetRelationshipHeadByCanonicalID` SHALL apply the same
main-preferring, explicit-id-wins resolution as graph objects, so a relationship forked
to a branch does not resolve non-deterministically.

#### Scenario: Forked relationship resolves to main

- **WHEN** a relationship has a main HEAD and a fork-copy HEAD sharing a `canonical_id`, in either UUID order
- **THEN** a canonical-id lookup with no branch context returns the main HEAD relationship

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
