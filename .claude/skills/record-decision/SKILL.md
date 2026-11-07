---
name: record-decision
description: Add an entry to docs/decisions.md for a design choice that the thesis leaves open or that deviates from it. Use whenever code makes such a choice — a default value, an interpretation of a requirement, a deviation from a figure or algorithm, a new dependency.
argument-hint: "[short title]"
---

# Record a design decision

Title: $ARGUMENTS

Append to the end of `docs/decisions.md`, keeping one blank line between
entries and LF line endings:

```markdown
## YYYY-MM-DD — <Short title>

**Decision:** What was decided, concretely enough to check in the code
(names of types, flags, values).

**Reason:** Why, with references to the thesis: section, figure, algorithm,
FR/NFR, scenario. Name the requirement the choice protects.

**Alternatives:** What else was considered and why it was not chosen.
```

Rules:
- Use today's date.
- One decision per entry; do not edit older entries, add a new one that
  supersedes them and say so.
- If the decision deviates from Chapter 5, start the Decision with
  "Deviation from <Figure/Algorithm/Section>:" so it is easy to find when the
  thesis is updated.
- Mention the entry in your final summary so the user can carry it into the
  thesis.
