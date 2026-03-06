---
name: openspec-change-reviewer
description: Review an OpenSpec change plan against the codebase to find logical errors, discrepancies, gaps, and areas needing detail. Use when the user asks to review a proposed change or verify if a design makes sense.
---

# OpenSpec Change Reviewer

This skill guides you through a rigorous, step-by-step review of an OpenSpec change (proposal, specs, design, tasks) to ensure there are no logical errors, discrepancies, or gaps, and that the plan perfectly aligns with the existing codebase.

## Input

Optionally specify a change name after triggering the review (e.g., "Review the add-auth change"). If omitted, check with the user or use `openspec list` to identify the most recently modified change.

## Review Steps

Follow these steps rigorously in order. Do not skip any phase.

### 1. Artifact Ingestion
Read the full context of the change. Use `openspec status --change "<name>" --json` to list artifacts.
- Read `proposal.md`
- Read all `specs/**/*.md`
- Read `design.md`
- Read `tasks.md`

### 2. Logical Consistency & Discrepancy Check
Analyze the artifacts internally for contradictions or missing links:
- **Proposal vs. Specs**: Does every capability listed in the proposal have a corresponding spec? Does the spec fulfill the proposal's "Why"?
- **Specs vs. Design**: Does the design explicitly address how to implement the requirements and scenarios in the specs?
- **Design vs. Tasks**: Do the tasks in `tasks.md` completely cover the architecture and decisions outlined in the `design.md`?
- **Logical Errors**: Are there impossible sequences, circular dependencies, or contradictory requirements?

### 3. Codebase Alignment Verification
Actively search the codebase to verify the assumptions made in the plan:
- **Component Verification**: Use `grep_search` and `codebase_investigator` to search for existing classes, functions, or files mentioned in the design/tasks. Do they exist as described?
- **Integration Points**: If the design integrates with existing modules, verify those modules expose the required interfaces.
- **Architectural Fit**: Does the proposed change violate any existing architectural patterns found in the codebase? 

### 4. Gap Analysis & Detail Enhancement
Put intense focus on finding missing elements that need more detail:
- **Missing Edge Cases**: Are error states, fallbacks, or network failures handled?
- **Missing Dependencies & Tooling**: Does the implementation require new libraries, system packages, or external tools to be installed during development or as part of the final feature? Are there explicit tasks to update dependency manifests (e.g., `package.json`, `go.mod`), setup scripts, Dockerfiles, or DB migrations that aren't listed in `tasks.md`?
- **Testability**: Are the specs testable? Do the tasks include specific testing strategies (unit, integration, e2e)?
- **Vagueness**: Spot instructions in the tasks or design that are too vague to be implemented directly without further decisions.

## Output format

Generate a comprehensive review report. Group your findings logically using the following structure:

### 📋 Executive Summary
A brief assessment of the overall quality and readiness of the change plan.

### 🔴 Logical Errors & Discrepancies
List any contradictions found between the artifacts (e.g., "Task 2.1 implements a cache, but the design doesn't mention one").

### 🏗️ Codebase Alignment Issues
List assumptions in the plan that contradict the current state of the codebase (e.g., "The design references `AuthService`, but in the codebase, it's called `SessionManager`").

### 🕳️ Gaps and Missing Elements
Highlight what is missing. Focus on DB migrations, edge cases, missing test coverage, missing development dependencies/tooling, or unhandled errors.

### 🔍 Areas Needing More Detail
List specific parts of the proposal, specs, design, or tasks that are too vague and need expanding before implementation begins. Include actionable suggestions on what to add.