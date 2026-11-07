---
name: implement-step
description: Implement one step of the drainsim roadmap end to end, following the thesis design — code, tests, GoDoc, decisions entry, status update and a short summary. Use when the user asks to build, add or continue a feature, component, command or milestone (M1–M8) of drainsim.
argument-hint: "[step, e.g. 'consolidate command' or 'Prometheus collector']"
---

# Implement a roadmap step

Step to implement: $ARGUMENTS
(If empty, take the first open item of "Next steps" in CLAUDE.md.)

## 1. Understand before coding

- Load the thesis-design skill. Name the requirements (FR/NFR), the section,
  figure or algorithm, and the component this step implements.
- Read the affected packages, their tests and `docs/decisions.md`.
- If the design leaves a choice open, choose the option closest to the thesis,
  and plan a decisions entry. If the choice changes behaviour the user would
  notice, stop and ask first, with the options.

## 2. Work on a branch

- `git switch develop && git pull`, then `git switch -c feat/<topic>`
  (`fix/` or `chore/` where it fits).
- Keep the step small: one component or one command per branch.

## 3. Code

- Respect Fig 5.1: analyses use `engine.Simulation` only; extend the facade if
  they need more state. When `Simulation` grows, update `fakeSim` in
  `internal/analyses/drainorder/drainorder_test.go`.
- Define an interface in the package that uses it; add
  `var _ Iface = (*Impl)(nil)` next to it.
- GoDoc in English on every exported identifier, starting with its name and
  citing the component and section, e.g. `(component PDB model, Section 5.3, FR10)`.
  Every new package gets a `doc.go`.
- Deterministic (NFR7): sort results; never rely on map order; no randomness;
  time comes from `cli.Deps.Now`.
- Errors with `fmt.Errorf("…: %w", err)` from the standard library; a failed
  check is `*reporter.CheckFailedError` (exit 1), other errors exit 2.
- No new third-party modules without a decisions entry.

## 4. Tests

- Unit tests next to the code. Engine and analysis tests use the real
  scheduler adapter, shared through `sync.OnceValues`.
- Test the thesis scenario the step is about (P…, S…, F…), with a comment
  naming it. Compute expected numbers by hand in the test.
- Run the go-check skill until everything passes.

## 5. Record and report

- Add decisions entries with the record-decision skill.
- Update the Status and Next steps in CLAUDE.md (update-status skill).
- Commit in small commits: `<package>: <what and why>`. Do not push unless
  the user asks.
- Finish with a short summary only: branch, commits, which checks passed,
  open questions. Long messages get cut off when the user pastes them.
