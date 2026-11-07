# CLAUDE.md

Guidance for Claude Code in this repository.

## Project

**drainsim** is a command-line tool that simulates changes of a Kubernetes
cluster offline, on a snapshot, before anyone touches the real cluster. It is
the practical part of Filip Kopecký's bachelor's thesis at FEL ČVUT
("Bachelor's Thesis: Drain Simulation Tool"). It answers three questions:

- **Q1 drain** – can these nodes be drained, and in what order? (`drainsim drain`)
- **Q2 consolidate** – which nodes can be removed, and what does it save in money and CO₂? (`drainsim consolidate`)
- **Q3 policy** – would a stricter Pod Security level break the next drain? (`drainsim policy`)

`drainsim snapshot` reads the cluster read-only and writes a sanitized JSON
snapshot; every other command works only on that file.

## The thesis is the specification

- **Chapter 5 (Design) is the source of truth**: its text, the requirements
  (FR1–FR36, NFR1–NFR17), the problems/scenarios (P1–P10, F1–, S1–S3), the use
  cases (UC1–UC15), the algorithms (Alg. 5.1–5.4) and the diagrams
  (Fig 5.1 components, 5.2 data model, 5.3 engine components, 5.4 one drain
  step, 5.6 policy impact).
- Implement exactly what the design says. Where the design is silent or the code
  must deviate, **add an entry to `docs/decisions.md`** (format below) and say so
  in your summary. Known deliberate deviations are already recorded there; read
  that file before changing behaviour.
- Chapter 6 (Implementation) of the thesis describes what is built; it is
  updated outside this repository.

## Commands

```bash
gofmt -l .            # must print nothing
go build ./...
go vet ./...
go test ./...         # tests use the real scheduler framework; they take a while
go build -o drainsim ./cmd/drainsim

./drainsim snapshot -o cluster.json [--kubeconfig F] [--context C]
./drainsim drain cluster.json [--nodes a,b | --selector k=v] [--strategy greedy|backtracking] [--max-attempts N] [--format text|json]
./drainsim policy cluster.json --config drainsim.yaml [--nodes … | --selector …] [--format …]
```

Always run all four checks (gofmt, build, vet, test) before reporting that a
change is done. `go build` does not compile `_test.go` files.

## Architecture

Each component of Fig 5.1 and Fig 5.3 is one package under `internal/`.

| Package | Component (thesis) | Pattern |
|---|---|---|
| `cli` | CLI | Command |
| `reporter` | Reporter (report model, text/JSON, exit codes) | – |
| `collector` | Snapshot collector (+ sanitization) | – |
| `snapshot` | Snapshot data model (Fig 5.2) | – |
| `config` | Configuration artifact (YAML) | – |
| `engine` | Simulation engine: `Engine` implements `Simulation` (ISimulation) | Facade |
| `engine/loader` | Snapshot loader → creates the cluster state | – |
| `engine/state` | Cluster state, `Memento` | Memento |
| `engine/eviction` | Eviction rules (kubectl drain filters) | – |
| `engine/pdb` | PDB model (Eviction API rules) | – |
| `engine/replacement` | Replacement model | Factory Method |
| `engine/scheduler` | Scheduler adapter (`Placement`, IPlacement) | Adapter |
| `engine/psa` | PSA checker (`PolicyChecker`, IPolicyChecker) | Adapter, Strategy |
| `engine/eventlog` | Event log | Observer |
| `analyses/drainorder` | Drain order search (Q1): greedy, backtracking | Strategy |
| `analyses/consolidation` | Consolidation (Q2), `CostModel` (ICostModel) | Strategy |
| `analyses/policyimpact` | Policy impact (Q3) | – |
| `estimator` | Cost and CO₂ estimator | – |

Rules that keep the code aligned with the diagrams:

- **Analyses see the engine only through `engine.Simulation`** (Fig 5.1). They
  never import `engine/state`. If an analysis needs data from the cluster state,
  add a method to the facade.
- **The CLI calls analyses and the reporter**, not engine internals; it only
  constructs the engine (composition root).
- **One drain step follows Fig 5.4** in `engine.drainPod`/`drainNode`:
  eviction rules → PDB model → evict → replacement → PSA check → scheduler →
  AddPod + Bind (+ `pdb.Recover`) / Pending / lost replica. Rounds with
  deferred pods; no progress → drain fails; refused pods → drain fails.
- Events go to observers (`Attach`/`Detach`). Analyses attach their own
  `eventlog.Log`; events of undone attempts must not reach a report.
- `engine.Checkpoint` = state Memento + replacement counter, so a restored run
  repeats replacement names (needed by Fig 5.6).

## Go conventions

- **Interface names without the `I` prefix** (`Simulation`, `Placement`,
  `PolicyChecker`, `CostModel`, `Collector`); the GoDoc names the design
  interface, e.g. `(ISimulation, Section 5.3)`.
- **An interface is defined by the package that uses it**, not by the one that
  implements it. Add compile-time checks next to the interface:
  `var _ Simulation = (*Engine)(nil)`.
- **GoDoc in English**, starting with the identifier name, and referencing the
  component, section and requirement it implements, e.g.
  `// Evict checks … (component PDB model, Section 5.3, FR10).`
  Every package has a `doc.go` with
  `// Package x … (component …, Section …). Main type: …`.
- **Determinism (NFR7)** is a hard requirement:
    - never depend on map iteration order; sort every returned list
      (pods by namespace then name, nodes by name; candidates by CPU, memory, name);
    - no randomness; replacement names use a counter (`…-sim0`);
    - no `time.Now()` in logic — the CLI passes time in via `cli.Deps.Now`.
- **State is never shared with the snapshot**: deep-copy (`DeepCopy()`) when
  objects enter `ClusterState` or a Memento, and when the facade returns a node.
- **Errors**: use the standard library `errors`/`fmt.Errorf("…: %w", err)`.
  Never import error packages from `k8s.io/kubernetes/cmd/...` (an IDE once
  auto-imported kubeadm's errors package). A failed check (drain/policy) is
  `*reporter.CheckFailedError` → exit 1; any other error → exit 2; success → 0
  (Section 5.7, FR35).
- **Offline (NFR3, NFR4)**: analyses and the engine make no network calls.
  Only `collector` talks to the Kubernetes API (and later Prometheus).
- **Reuse upstream code instead of re-implementing it (NFR5)**: scheduler
  plugins via the scheduler framework, Pod Security via
  `k8s.io/pod-security-admission`, pod requests via `k8s.io/component-helpers`.
  Rules copied from upstream (kubectl drain filters, Eviction API) keep their
  order and cite the upstream file in a comment.
- **Dependencies**: `k8s.io/kubernetes` v1.36.5 with all staging modules
  replaced by v0.36.5 in `go.mod`. Do not add new third-party modules without a
  decisions entry. YAML uses `sigs.k8s.io/yaml` (JSON tags, `UnmarshalStrict`).

## Tests

- Unit tests next to the code. Engine and analysis tests use the **real
  scheduler adapter**, shared per package:
  `var adapter = sync.OnceValues(func() (*scheduler.Adapter, error) { return scheduler.New(context.Background()) })`.
- Pods in tests must be `Running` and `Ready` with a controller reference,
  otherwise the PDB and eviction rules treat them differently.
- `analyses/drainorder` tests use `fakeSim`. **When `engine.Simulation` gets a
  new method, add a trivial implementation to `fakeSim`**, or `go test` will not
  compile.
- Compare floats with a tolerance.

## Repository rules

- Branches: `develop` is the integration branch; work on `feat/<topic>`,
  `fix/<topic>`, `chore/<topic>`, and open a PR into `develop`. Keep PRs small,
  one component or step at a time.
- Commit messages: `<package>: <what and why>`, e.g.
  `estimator: estimate money and CO2 saved by removing a node`.
- **Line endings LF, encoding UTF-8.** Never create files with PowerShell
  `echo … >` (it writes UTF-16 with BOM). `.gitattributes` must be the single
  ASCII line `* text=auto eol=lf`.
- `go.sum` is committed. Binaries (e.g. `kubectl`, `drainsim`) are not.
- Do not commit unfinished files that do not compile (e.g. stubs with empty
  function bodies); keep them local until they build.

## docs/decisions.md format

```markdown
## YYYY-MM-DD — Short title

**Decision:** What was decided, concretely.

**Reason:** Why, with references to the thesis (sections, FR/NFR, scenarios).

**Alternatives:** What else was considered and why it was not chosen.
```

## Working with the user

- Filip is new to Go and writes the thesis text himself. He talks in Czech;
  code, comments and commit messages are in English.
- Prefer doing the whole task (code, tests, decisions entry) over leaving
  parts to the user, unless asked otherwise.
- Long messages get cut off when pasted. **End with a short summary**: the
  commits made and which of gofmt/build/vet/test passed, plus anything that
  needs a decision.
- Every change should be reviewable against the thesis: mention the section,
  algorithm, figure or requirement it implements.

## Status (November 2026)

| Milestone | State |
|---|---|
| M0 scheduler spike (`cmd/spike/scheduler`) | done |
| M1 snapshot: collector, sanitization, writer, loader, read-only RBAC | done (collector unit test and kind check pending) |
| M2 engine: state + Memento, eviction, replacement, scheduler adapter, event log, facade | done |
| M3 PDB model | done |
| M4 report + `drain` command | done (report tests pending) |
| M5 drain order search: greedy, backtracking, `--strategy`, `--max-attempts` | done |
| M6 policy impact: config, running-pod check, Alg. 5.4, `policy` command | done |
| M7 consolidation (Alg. 5.3), lower bound, cost/CO₂ estimator | in progress |
| M8 scenario clusters (kind) P1–S3, RQ1 comparison with real drains, benchmarks (NFR9), CI | not started |

Next steps in M7, in this order:
1. `drainsim consolidate` command and its report section (removable nodes,
   kept nodes with reasons, lower bound, savings; Section 5.7, UC10–UC12);
   example configuration with Azure prices and Cloud Carbon Footprint
   coefficients.
2. Usage profiles per workload and container in the snapshot (Fig 5.2).
3. Collector: read usage from Prometheus (FR20) — p95 CPU and max memory per
   container, assigned to the workload via `kube_pod_owner`; develop against a
   fake Prometheus (`httptest`).
4. Right-sizing (FR21–FR23): engine method to replace container requests
   (ISimulation: "replace the requests of containers"), then consolidate again.

Known open items: `.gitattributes` is UTF-16 on `develop` and must be rewritten
as ASCII; Fig 5.4 still lacks the "refused pods → drain fails" branch (diagram
only).