---
name: thesis-notes
description: Draft text for Chapter 6 (Implementation) of the thesis about a change in drainsim, in natural academic English. Use when the user asks for thesis text, implementation notes or an explanation of how a component was built.
argument-hint: "[component, branch or commit range]"
context: fork
agent: thesis-writer
background: false
---

Write the Chapter 6 text for: $ARGUMENTS
(If empty: the changes on the current branch compared with `develop`.)

1. Read the code, its tests and the related entries in `docs/decisions.md`.
2. Load the thesis-design skill to cite the right sections, figures,
   algorithms and requirements.
3. Draft the text following your writing rules. Explain what was built and
   why, the decisions where the design was silent, and how it is tested.
   Do not repeat Chapter 5; refer to it.

Return the draft as Markdown with a `###` heading for the subsection, then a
list of the decisions entries and tests it relies on, so the user can check it.
