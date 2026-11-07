---
name: thesis-design
description: The design of drainsim from Chapter 5 of the bachelor's thesis — requirements FR1–FR36 and NFR1–NFR22, components and patterns, the drain step of Figure 5.4 and Algorithms 5.1–5.4. Use whenever you implement, change, test or review behaviour of drainsim, or need to know which requirement, figure or section a piece of code belongs to.
user-invocable: false
---

# drainsim design (thesis Chapter 5)

The thesis is the specification. Code follows its text, requirements and
diagrams. Where the design is silent or code must differ, record it in
`docs/decisions.md` (record-decision skill) and say so in your summary.

Before changing behaviour:

1. Find the requirement(s) in [requirements.md](requirements.md).
2. Find the component, pattern and algorithm in [components.md](components.md).
3. Read `docs/decisions.md`: many details were already decided there.
4. Cite the section, figure, algorithm or requirement in GoDoc and in the
   commit or PR description.

## The rules that matter most

- **Fig 5.1:** analyses see the engine only through `engine.Simulation`; the CLI
  calls analyses and the reporter; only the collector touches the cluster.
- **Fig 5.4:** one drain step is, in this order: eviction rules → PDB model →
  evict → replacement → PSA check → scheduler → bind, mark Ready, recover the
  budget. Deferred pods go to the next round; no progress fails the drain;
  refused pods fail the drain.
- **NFR5:** reuse upstream Kubernetes code (scheduler framework, Pod Security
  Admission, component-helpers, kubectl and Eviction API rules in their order).
- **NFR7:** same input, same output. Sort everything; no map order; no
  randomness; no `time.Now()` in logic.
- **NFR1–NFR4, NFR11:** read-only snapshot; simulations offline; nothing leaves
  the machine.

## Where the thesis lives

The thesis is a Claude Doc outside this repository ("Bachelor's Thesis: Drain
Simulation Tool"). The files in this skill summarise what the code needs. If a
detail is missing here, ask the user instead of guessing.
