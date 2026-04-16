# Gas City

Gas City is an orchestration-builder SDK — a Go toolkit for composing
multi-agent coding workflows. It extracts the battle-tested subsystems from
Steve Yegge's Gas Town (github.com/steveyegge/gastown) into a configurable
SDK where **all role behavior is user-supplied configuration** and the SDK
provides only infrastructure. The core principle: **ZERO hardcoded roles.**
The SDK has no built-in Mayor, Deacon, Polecat, or any other role. If a
line of Go references a specific role name, it's a bug.

You can build Gas Town in Gas City, or Ralph, or Claude Code Agent Teams,
or any other orchestration pack — via specific configurations.

**Why Gas City exists:** Gas Town proved multi-agent orchestration works,
but all its roles are hardwired in Go code. Steve realized the MEOW stack
(Molecular Expression of Work) was powerful enough to abstract roles into
configuration. Gas City extracts that insight into an SDK where Gas Town
becomes one configuration among many.

## Development approach

**TDD.** Write the test first, watch it fail, make it pass. Every package
has `*_test.go` files next to the code. Integration tests that need real
infrastructure (tmux, filesystem) go in `test/` with build tags.

**The spec is a reference, not a blueprint.** When the DX conflicts
with the spec, DX wins. We update the spec to match.

## Architecture

**Work is the primitive, not orchestration.** Gas City's orchestration
is a thin layer atop the MEOW stack (beads → molecules → formulas).
The work definition and tracking infrastructure is what matters; the
orchestration shape is configurable on top.

### The nine concepts

Gas City has five irreducible primitives and four derived mechanisms.
Removing any primitive makes it impossible to rebuild Gas Town. Every
mechanism is provably composable from the primitives.

**Five primitives (Layer 0-1):**

1. **Agent Protocol** — start/stop/prompt/observe agents regardless of
   provider. Identity, pools, sandboxes, resume, crash adoption.
2. **Task Store (Beads)** — CRUD + Hook + Dependencies + Labels + Query
   over work units. Everything is a bead: tasks, mail, molecules, convoys.
3. **Event Bus** — append-only pub/sub log of all system activity. Two
   tiers: critical (bounded queue) and optional (fire-and-forget).
4. **Config** — TOML parsing with progressive activation (Levels 0-8 from
   section presence) and multi-layer override resolution.
5. **Prompt Templates** — Go `text/template` in Markdown defining what
   each role does. The behavioral specification.

**Four derived mechanisms (Layer 2-4):**

6. **Messaging** — Mail = `TaskStore.Create(bead{type:"message"})`.
   Nudge = `AgentProtocol.SendPrompt()`. No new primitive needed.
7. **Formulas & Molecules** — Formula = TOML parsed by Config. Molecule =
   root bead + child step beads in Task Store. Wisps = ephemeral molecules.
   Orders = formulas with gate conditions on Event Bus.
8. **Dispatch (Sling)** — composed: find/spawn agent → select formula →
   create molecule → hook to agent → nudge → create convoy → log event.
9. **Health Patrol** — ping agents (Agent Protocol), compare thresholds
   (Config), publish stalls (Event Bus), restart with backoff.

### Layering invariants

1. **No upward dependencies.** Layer N never imports Layer N+1.
2. **Beads is the universal persistence substrate** for domain state.
3. **Event Bus is the universal observation substrate.**
4. **Config is the universal activation mechanism.**
5. **Side effects (I/O, process spawning) are confined to Layer 0.**
6. **The controller drives all SDK infrastructure operations.**
   No SDK mechanism may require a specific user-configured agent role.

### Progressive capability model

Capabilities activate progressively via config presence.

| Level | Adds                    |
|-------|-------------------------|
| 0-1   | Agent + tasks           |
| 2     | Task loop               |
| 3     | Multiple agents + pool  |
| 4     | Messaging               |
| 5     | Formulas & molecules    |
| 6     | Health monitoring       |
| 7     | Orders             |
| 8     | Full orchestration      |

## Design decisions (settled)

These decisions are final. Do not revisit them.

- **City-as-directory model.** A city is a directory on disk containing
  `city.toml`, `.gc/` runtime state, and `rigs/` infrastructure.
- **Fresh binary, not a Gas Town fork.** We build `gc` from scratch.
- **TOML for config.** `city.toml` is the single config file.
- **Tutorials win over spec.** When the spec disagrees, we update the spec.
- **No premature abstraction.** Don't build interfaces until two
  implementations exist.
- **Mayor is overseer, not worker.** The mayor plans; coding agents work.
- **`internal/` packages for now.** SDK exports (`pkg/`) are future work.
  Everything is private to the `gc` binary until the API stabilizes.
- **ZERO hardcoded roles.** Roles are pure configuration. No role name
  appears in Go source code.

## Decision frameworks

- **`engdocs/contributors/primitive-test.md`** — The Primitive Test: three necessary
  conditions (Atomicity + Bitter Lesson + ZFC) for whether a capability
  belongs in the SDK vs the consumer layer. Apply this before adding any
  new primitive.
- **`engdocs/archive/backlogs/worktree-roadmap.md`** — Worktree isolation roadmap, polecat
  lifecycle analysis, and Gas Town cleanup bug lessons.

## Key design principles

- **Zero Framework Cognition (ZFC)** — Go handles transport, not reasoning.
  If a line of Go contains a judgment call, it's a violation. **The ZFC
  test:** does any line of Go contain a judgment call? An `if stuck then
  restart` is framework intelligence. Move the decision to the prompt.
- **Bitter Lesson** — every primitive must become MORE useful as models
  improve, not less. Don't build heuristics or decision trees.
- **GUPP** — "If you find work on your hook, YOU RUN IT." No confirmation,
  no waiting. The hook having work IS the assignment. This is rendered into
  agent prompts via templates, not enforced by Go code.
- **Nondeterministic Idempotence (NDI)** — the system converges to correct
  outcomes because work (beads), hooks, and molecules are all persistent.
  Sessions come and go; the work survives. Multiple independent observers
  check the same state idempotently. Redundancy is the reliability mechanism.
- **No status files — query live state.** Never write PID files, lock files,
  or state files to track running processes. Always discover state by querying
  the system directly (process table, port scans, `ps`, `lsof`). Status files
  go stale on crash and create false positives. The process table is the
  single source of truth for "what is running."
- **SDK self-sufficiency.** Every SDK infrastructure operation (gate
  evaluation, health patrol, bead lifecycle, order dispatch) must
  function with only the controller running. No SDK operation may
  depend on a specific user-configured agent role existing. The
  controller drives infrastructure; user agents execute work. Test:
  if removing a `[[agent]]` entry breaks an SDK feature, it's a
  violation.

## What Gas City does NOT contain

These are permanent exclusions, not "not yet." Each fails the Bitter
Lesson test — it becomes LESS useful as models improve.

- **No skills system** — the model IS the skill system
- **No capability flags** — a sentence in the prompt is sufficient
- **No MCP/tool registration** — if a tool has a CLI, the agent uses it
- **No decision logic in Go** — the agent decides from prompt and reality
- **No hardcoded role names** — roles are pure configuration

## Code conventions

- Unit tests next to code: `config.go` → `config_test.go`
- `t.TempDir()` for filesystem tests
- Integration tests use `//go:build integration`
- `cobra` for CLI, `github.com/BurntSushi/toml` for config
- Atomic file writes: temp file → `os.Rename`
- No panics in library code — return errors
- Error messages include context: `fmt.Errorf("adding rig %q: %w", name, err)`
- Role names never appear in Go code. If you're writing `if role == "mayor"`,
  it's a design error.
- **Tmux safety:** Never run bare `tmux kill-server` as cleanup. Never kill the
  default tmux server. If tmux cleanup is required, target only the known
  city/test socket explicitly with `tmux -L <socket> ...`, or prefer `gc stop`
  for city shutdown. Treat personal tmux servers as out of bounds.
- **Adding agent config fields:** When adding a field to `config.Agent`,
  also add it to `AgentPatch`, `AgentOverride`, their apply functions
  (`applyAgentPatch`, `applyAgentOverride`), and the `poolAgents` deep-copy
  in `cmd/gc/pool.go`. `TestAgentFieldSync` enforces this for the struct
  definitions; the apply functions and pool deep-copy must be checked
  manually.

- `TESTING.md` — testing philosophy and tier boundaries. Read before writing any test.

## Code quality gates

Before considering any task complete:

- `go test ./...` passes
- `go vet ./...` clean
- Every exported function has a doc comment
- No premature abstractions
- Tests cover happy path AND edge cases


<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:ca08a54f -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

## Session Completion

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd dolt push
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
<!-- END BEADS INTEGRATION -->
