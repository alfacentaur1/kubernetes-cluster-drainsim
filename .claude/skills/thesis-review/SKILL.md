---
name: thesis-review
description: Review the current branch or a pull request against the thesis design (Chapter 5) and the Go conventions of the repository, and report problems by severity. Use before opening or merging a PR, or when the user asks whether a change follows the thesis.
argument-hint: "[branch or PR number, default: current branch]"
context: fork
agent: thesis-reviewer
background: false
---

Review $ARGUMENTS (or the current branch if empty) against `develop`.

1. Collect the change: `git diff develop...HEAD --stat` and the full diff; for
   a PR number use `gh pr diff <n>` if `gh` is available.
2. Load the thesis-design skill and read `docs/decisions.md` and CLAUDE.md.
3. Check every item of your review checklist.
4. Run the go-check skill.

Return the report in the format of your instructions. Do not change files.
