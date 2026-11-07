---
name: update-status
description: Update the Status table and the Next steps in CLAUDE.md after a step is finished. Use at the end of every implemented step or merged PR.
---

Update the `## Status` section of CLAUDE.md:

- Mark the milestone rows that changed (done / in progress / pending, with what
  is still open in brackets).
- Rewrite "Next steps" as an ordered list of the remaining items, the next one
  first, each with the thesis section or requirement it implements.
- Move items that are now done out of "Known open items"; add new ones that
  you found (bugs postponed, diagrams that no longer match the code).
- Change the month in the heading if needed.

Keep the rest of CLAUDE.md unchanged. Commit as
`docs: update status in CLAUDE.md`.
