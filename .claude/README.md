# Claude Code setup for drainsim

This folder configures [Claude Code](https://code.claude.com/docs) for the
drainsim repository. Together with `CLAUDE.md` in the repository root it turns
the bachelor's thesis (Chapter 5, Design) into working instructions for the
agent, so every change can be traced back to a requirement, figure or
algorithm.

```
.claude/
├── settings.json            permissions: safe Go and git commands allowed,
│                            cluster and push commands ask, destructive ones denied
├── agents/                  subagents with their own context and tools
│   ├── thesis-reviewer.md   reviews changes against the thesis design (read-only)
│   ├── go-reviewer.md       reviews Go correctness and readability (read-only)
│   ├── scenario-runner.md   runs kind scenarios against real kubectl drains
│   └── thesis-writer.md     drafts Chapter 6 text in academic English
└── skills/
    ├── thesis-design/       background knowledge: FR/NFR, components, Fig 5.4,
    │   ├── SKILL.md         Algorithms 5.1–5.4 (loaded automatically)
    │   ├── requirements.md
    │   └── components.md
    ├── implement-step/      /implement-step   one roadmap step end to end
    ├── go-check/            /go-check         gofmt, build, vet, test, line endings
    ├── record-decision/     /record-decision  entry in docs/decisions.md
    ├── kind-scenario/       /kind-scenario    reproducible scenario on kind (NFR20, RQ1)
    ├── thesis-review/       /thesis-review    review a branch or PR (runs thesis-reviewer)
    ├── thesis-notes/        /thesis-notes     draft Chapter 6 text (runs thesis-writer)
    └── update-status/       /update-status    status and next steps in CLAUDE.md
```

## Typical workflow

1. `/implement-step consolidate command` — the agent reads the design, writes
   code and tests, records decisions and updates the status.
2. `/go-check` — the quality gate.
3. `/thesis-review` — an isolated reviewer checks the branch against Chapter 5.
4. `/kind-scenario p7 create` — reproduce a thesis scenario on a real cluster
   and compare with `kubectl drain`.
5. `/thesis-notes` — draft the matching subsection of Chapter 6.

## How the pieces fit

- **CLAUDE.md** holds facts that apply to every session: architecture,
  conventions, environment, status.
- **Skills** hold procedures and reference material that load only when
  needed, so they cost no context until used.
- **Subagents** run reviews and long tasks in their own context, so the main
  session stays focused; the reviewers have no write access.
- **settings.json** keeps the agent away from the user's kubeconfig and from
  destructive git commands.
