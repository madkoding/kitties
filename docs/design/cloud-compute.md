# Local or cloud: where a kitty's desktop runs

Status: design proposal. Steps 1 and 2 (the provider interface, a fake and the contract tests in
`internal/cloud`, and the config) are done; nothing reaches the gateway yet.

## Goal

Today every kitty's body is a Docker or Podman container on the machine that runs the gateway.
This plan adds a second place to run it: a persistent cloud VM. A person chooses **Local** or
**Cloud** for the whole deployment and can override it per kitty. Brain (the LLM provider) and
compute (where the desktop runs) stay independent.

## Reference

`Yhozen/kitties` (a fork called Puppies) moved the bodies to persistent Boat VMs behind a
Cloudflare gateway written in TypeScript and Effect. We do not port that code (our gateway is a
single Go process with the standard library only). We reuse its decisions, which its
`docs/CLOUD-PLAN.md` records, and the license is compatible (GPL-3.0-or-later; keep notices).

Reused decisions:

- One compute kind per kitty, stored with owner, remote id, generation, checkpoint and pending
  operations.
- The create request and its idempotency key are saved before sending. An ambiguous answer never
  creates a blank replacement.
- Sleep drains work, flushes files, stops the VM and saves a permanent checkpoint. Stop disables
  wake until an explicit Start. Remove keeps the checkpoint; purge is a separate action.
- Defaults: small VM (2 vCPU, 4 GB), at most five running desktops, idle sleep after 15 minutes,
  a TTL renewed while active so orphan compute expires.
- Unknown outcomes are parked for the owner, never replayed.

Known limits there (measured by that project): p95 non-model overhead of 1.4 s, the live view
falling back from rectangles to stream to polling, and a first OS install of several minutes.

## Design

### Seam

`orchestrator.Containers` (`internal/orchestrator/orchestrator.go`) is already the interface the
orchestrator needs: Up, Start, Stop, Remove, Exec, ExecInput, Screenshot, Stream, Tunnel, List,
Pull, ImageStale. A cloud backend implements the same interface, so chat, goals and the live view
do not change.

Rename the concept to `Compute` in docs and, once a second backend exists, in code. The first
step keeps the interface as it is.

### Configuration

```yaml
containers:
  runtime: auto            # auto | docker | podman | cloud
cloud:
  provider: ""             # a provider name; empty until one is configured
  max_running: 5
  idle_minutes: 15
  size: small
```

- `runtime: auto` keeps today's behaviour (a local engine if there is one).
- Each kitty may set `compute: local|cloud`; unset follows the deployment.
- Credentials for the provider go in the vault, never in the config file, the image or the guest
  environment.

### Provider interface

Do not hard-wire one vendor. A small `internal/cloud` package defines what a provider offers
(create with idempotency key, get, sleep, wake, restore from checkpoint, delete, exec, screenshot,
stream, tunnel). Boat is the first implementation to try because the reference project proved it;
Fly, Hetzner or others fit the same shape.

### State

Per kitty, persisted next to the existing kitty record: `compute`, `remote_id`, `generation`,
`checkpoint`, `pending` operations. On gateway restart, reconcile pending operations against the
provider before doing anything else.

### UI and CLI

- Settings: Local or Cloud for the deployment, with a plain note on cost and that cloud needs a
  provider credential.
- Kitty drawer: the kitty's compute, its state (running, asleep, stopped) and its estimated cost.
- CLI: `kitties compute <kitty> local|cloud` and the same choice at `kitties new`.
- Moving a kitty between local and cloud is an explicit file handoff of `/home/kitty/work`, not a
  live migration.

## Steps

1. **Interface and fake (done).** `internal/cloud` has the `Provider` interface, `Ensure` (a lost
   answer finds the machine by key; an ambiguous one creates nothing), an in-memory `Fake` and
   `Contract`, the rules every provider's tests must run. Pin the `Compute` contract and add a fake backend. Cover it in
   `tests/contract`-style cases first (create twice with one key makes one machine; an ambiguous
   answer makes none; restart reconciles).
2. **Config and per-kitty field (done).** `containers.runtime: cloud`, `compute` per kitty, the
   `cloud` block (provider `boat`, 1 to 5 running, idle minutes, size), validation and docs in
   `docs/CONFIGURATION.md`. The gateway does not read them yet.
3. **First provider (Boat client done).** `cloud.Boat` implements `Provider` over Boat's API:
   create (idempotency key, ownership labels), get, list (paged; a partial inventory is an
   error), orderly sleep with a permanent checkpoint, wake, delete. It never touches a machine
   without this deployment's label. Tests run the contract against an in-memory Boat. Still to
   do: restore into a new machine, screenshot, exec and stream through the guest daemon, and a
   canary with a real key.
4. **Lifecycle.** Idle sleep, activity leases (chat, goals, viewer and control hold a machine
   awake; dashboard polling does not), TTL renewal, explicit stop.
5. **Persistence.** Permanent checkpoint per kitty, restore into a new VM, export of the
   workspace.
6. **UI and CLI.** The choice, state and cost display above.
7. **Canary, then five.** Run one real cloud kitty first, then up to five. Measure latency,
   storage and charges, and record them in this document before documenting cloud as supported.

## Suggestions folded into the plan

- Contract tests before code, like the window manager refactors.
- Provider interface instead of a single vendor.
- Cost estimate per kitty shown in the UI and a budget check before creating a VM.
- Canary before opening the feature up.
- No silent fallback: if cloud is chosen and the provider is down, the kitty reports it; it does
  not start a local container behind the person's back.

## Open questions

- Which provider first, and who pays for the canary? Decide before step 3. Decided: Boat. Who pays for
  the canary is still open.
- Does the live view need the same rectangle protocol over the provider's network path, or is the
  existing stream enough?
- Where do cloud credentials live for the desktop and Android apps that talk to a remote gateway?
