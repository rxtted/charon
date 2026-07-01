# charon design

## overview

charon is the homelab's alerting hub: a discord bot that receives alert events from the monitoring stack and app emitters, tracks each condition as a live incident, and renders it as an interactive message you can acknowledge, snooze, or resolve. it replaces apprise, which is a stateless webhook fan-out with no memory of what it sent.

apprise did one thing: take a tagged `title`/`body` post and forward it to the matching discord webhook. that is all a webhook can do. it cannot dedup a rule that fires every interval, it cannot tell you an alert resolved, it cannot hold an acknowledgement, and it cannot take a button click, because a webhook is send-only. the cost of that ceiling is a channel that fills with duplicate firings and stale alerts nobody cleared, and no way to act on an alert from inside discord.

charon is a bot, not a webhook, so it holds a gateway connection and owns state. the model is one incident per condition: the first firing posts a message, repeat firings edit it in place, a resolve deletes it. the channel is a live board of what is on fire right now, not a scrolling log. the buttons work because charon remembers which discord message maps to which incident and what you did to it.

this is the notification layer of the fleet telemetry and alerting design (`rotten-division/homelab`, `docs/specs/2026-07-01-fleet-telemetry-alerting-design.md`). that spec fixes grafana as the alert source, discord as the sink, and the function-tag routing; charon is the piece between them, replacing the apprise router it names.

## goals

- one incident per condition, with dedup so a rule firing every scrape interval stays one message.
- a live board: only currently-active incidents are visible; resolving removes the message.
- firing to resolved lifecycle tracked, so an alert that clears disappears on its own.
- act from discord: acknowledge (stop nagging me), snooze (remind me later), resolve (clear it now).
- severity-coloured, readable messages with the detail inline and a deep link back to the source.
- routing by function tag to a configurable discord channel, so the infra/media split stays config, not code.
- a clean ingest boundary: one shared event type, sources plugged in as adapters, so a new emitter format is a package, not a rewrite.
- durable state across restarts: acknowledgements, snoozes, and the message map survive a redeploy.

## non-goals

- charon does not evaluate metrics or thresholds. grafana owns alerting logic; charon owns notification and incident state. it never queries prometheus.
- no correlation or grouping in v1. one incident is one message. the event carries a `GroupKey` field left dormant for a future threading model (see designing for future correlation), but nothing reads it yet.
- no silence. acknowledge and snooze cover the "stop telling me" need; a maintenance-window mute across many conditions is not built.
- not a second alert history store. closed incidents live in charon's own database for MTTR, but the authoritative telemetry and its retention stay in prometheus/grafana.
- no high availability. charon rides triton, the single point of failure the core spec already accepts for dns and the proxy.
- charon is not a general notification service. it is the fleet's alerting hub. arbitrary "send me a message" traffic is out of scope.
- no authenticated ingest boundary by default. the ingest endpoint trusts the internal vlan, the posture apprise held and the one the homelab firewall design takes for internal hosts. an optional static bearer token is supported as a second check, but per-emitter signing is deliberately not built (see ingest and adapters).

## constraints carried from the homelab

- discord is the notification sink. one webhook or bot per function, secrets in the gitignored `.env`, placeholders in the repo mirror, keepassxc the master.
- images track `:latest` per the no-pin standard. that standard is for docker base images; charon's own go module dependencies are pinned in `go.mod` as normal, and disgo specifically is pinned because it takes breaking changes on minor versions (see discord library).
- charon runs in triton's existing `monitoring` compose stack, not a new stack, on the pi 5 (arm64).
- triton is the single point of failure. if it drops, charon drops with it, the same accepted gap the alerting spec documents. the future callisto out-of-band emitter is the backstop, unchanged by this design.

## architecture

ports and adapters. the core sees one `Event` type and nothing about where it came from or where it is rendered. two sides plug in: inbound adapters that turn a request into an `Event`, and the discord side that renders incidents and feeds button clicks back.

```
 emitters (event json POST):
   grafana contact point ─┐
   titan arr/sab scripts ─┼─► charon /ingest ─► adapter registry ─► Event ──┐
   triton unbound unit   ─┘                     (native now,               │
                                                  jellyfin etc. later)      │
                                                                            ▼
                                                    incident core (state machine)
                                                          │           ▲
                                                          │           │  Acknowledge/
                                              (Notifier)  │           │  Snooze/Resolve
                                                          ▼           │
                                                    discord bot ──────┘
                                                     per-channel send queue
                                                          │        ▲ button interactions
                                                          ▼        │ (gateway, in)
                                                    discord channels (live board)

  state: SQLite (incidents, acks, snoozes, message-id map, closed history)
```

ownership, one owner per concern:

- **ingest** owns turning an inbound HTTP request into an `Event` by asking the adapter registry. it owns nothing about incident state.
- **the adapter registry** owns source-format knowledge. each source is a package implementing one `Adapter`, self-registering at init. the core imports the `Event` type and the `Adapter` interface, never a concrete adapter.
- **the incident core** owns the state machine: dedup, lifecycle, acknowledgement, snooze, re-notification timing. it is the single source of truth for what an incident is and what happens to it.
- **the store** owns persistence. SQLite is the only durable state. the core reads and writes it; nothing else does.
- **the discord side** owns rendering (`Event`/incident to a Components V2 message), the send queue, and receiving interactions. it implements the `Notifier` port the core calls, and it calls the core's `Acknowledge`/`Snooze`/`Resolve` methods when a button is clicked. it owns no incident policy.

invariants:

- one active incident per `DedupKey`. a repeat firing never creates a second message.
- the discord channel shows only active incidents. a resolved incident's message is deleted, not edited to a resolved state.
- charon owns re-notification cadence. grafana sends firing and resolved edges; charon decides whether a still-firing incident pings you again. this is what makes acknowledge and snooze mean anything.
- transport does not own policy. the discord handler translates a click into a core call; it does not decide what acknowledge does.

## the event

the one type the core sees. every emitter and adapter produces this.

```go
type Status string   // "firing" | "resolved"
type Severity string // "info" | "warning" | "critical"

type Event struct {
    Source   string            // emitter: "grafana", "arr", "sabnzbd", "unbound", ...
    DedupKey string            // stable identity of the condition; repeat firings share it
    GroupKey string            // reserved for future correlation; unused in v1
    Status   Status
    Severity Severity          // drives the message accent colour; defaults to warning if unset
    Channel  string            // routing tag: infra | network | media | downloads | ...
    Title    string            // one-line human summary, sentence case
    Body     string            // secondary detail, optional
    Host     string            // originating host, optional
    Link     string            // deep link back to grafana panel or service ui, optional
    Labels   map[string]string // structured detail, rendered as fields
    Time     time.Time
}
```

`DedupKey` is the spine. it is the stable identity of the underlying condition, so firing then resolved then firing again is one incident lifecycle, and a grafana rule re-firing every interval maps to the one message already on the board. an emitter sets it (grafana uses the rule uid plus the alert's label fingerprint); if it is missing, ingest derives one from `Source`, `Channel`, and `Title`. a derived key is a fallback, not the intended path, because a title that changes wording between firings would split into two incidents. controlled emitters should set it explicitly.

`Channel` is the function tag, looked up in the routing config to find the discord channel. it is a routing key, not a discord id, so the tag-to-channel mapping stays config.

`Severity` sets the accent colour. emitters set it; grafana rules gain a `severity` label as part of the repoint. absent, charon treats it as warning.

## ingest and adapters

one inbound HTTP surface, `POST /ingest`, on a port published to the vlan and loopback the way apprise was, so grafana (over the docker network), the titan scripts (over the vlan), and the triton unbound unit (over loopback) can all reach it.

the adapter interface:

```go
var ErrNotMatched = errors.New("adapter did not match")

type Adapter interface {
    Name() string
    Path() string // the HTTP path this adapter owns, e.g. "/ingest"
    // Match parses a request already routed to this adapter's path into an Event,
    // or returns ErrNotMatched if the body is malformed for this adapter.
    Match(r *http.Request) (Event, error)
}
```

adapters self-register into a package-level registry at init, each declaring the path it owns. routing is by path, not by sniffing the body in registration order: the native adapter owns `POST /ingest`, and a future third-party adapter (the jellyfin plugin, say) owns its own path like `POST /webhook/jellyfin`. a request reaches exactly one adapter, deterministically, so a second adapter can never shadow or mis-claim a controlled emitter's payload. within its path the adapter parses the body and returns the `Event`, or `ErrNotMatched` if the body is malformed for it, which is a 4xx and a log line, not a silent drop: a controlled emitter posting a shape its own adapter cannot parse is a bug worth surfacing.

ingest caps the request body size before reading, so a malformed or oversized post cannot exhaust memory. an optional static bearer token, when configured, is required on every request and checked before routing; unconfigured, the endpoint trusts the internal vlan the way apprise did. per-emitter signing is a deliberate non-goal.

v1 has one adapter, **native**, which parses the `Event` json directly. everything charon controls (the grafana contact-point template, the rewritten arr/sab/unbound scripts) emits that schema, so they need no bespoke adapter. the registry exists for the sources charon does not control: a rigid third-party webhook like the jellyfin notification plugin gets its own adapter package that matches its payload and maps it onto an `Event`, without the core learning anything about jellyfin. that is the whole reason for the registry, and it is the seam the next integration uses.

## incident lifecycle

an `Incident` is the core's stateful object, keyed by `DedupKey`, backed by a SQLite row. it carries the event fields plus the discord message id, timestamps (created, acked, snoozed-until, last-notified, resolved), and who acked or snoozed it.

- **firing, no active incident for the key**: create the incident, render and post the message, store the returned message id.
- **firing, active incident exists**: update the fields, stamp `last_seen_firing`, and edit the message in place if anything visible changed (a severity change flips the accent amber to red). never a second message. a grafana rule re-firing every interval lands here; the repeat is a liveness heartbeat, not a reason to re-notify. re-notification is charon's own decision, below.
- **resolved edge, or the resolve button**: delete the discord message, stamp `resolved_at`, keep the row as closed history, cancel its timers. the board loses the message; the database keeps the record.
- **acknowledge**: stamp `acked_by`/`acked_at`, edit the message to show it, cancel re-notification. the incident stays active; it just stops nagging. if it later resolves, it resolves normally; a re-fire after resolve is a new incident and does not inherit the ack.
- **snooze(duration)**: stamp `snoozed_until = now + duration`, edit the message to show it, and schedule a re-notification at that time if the incident is still active then.

re-notification: charon owns it, grafana does not. a background sweep re-pings an incident that is active, not acknowledged, not snoozed, and last notified longer ago than the configured interval (default a few hours). because the board holds one message per incident and there are no threads to orphan, a re-ping is a delete-and-repost: the stale message is removed and a fresh one posted at the bottom, resurfacing the incident with a new notification and keeping the one-message-per-incident invariant. reposts do not fire in an instant batch: they go through the same per-channel paced queue as every other send (see sending and rate limits), jittered and rate-limited, and re-notification pauses entirely while the discord path is degraded or replaying a backlog. so a wave of synchronised incidents or snooze expiries cannot storm a channel or trip the 429 ban. a snooze expiring is the same paced repost, driven by the timer.

one-shot events (sabnzbd failure, an unbound unit entering failed) never send a resolved edge. they are ordinary firing incidents; they leave the board only when you click resolve. there is no auto-expiry in v1, by decision: a failed download or a failed unit is something you should see and clear, not something that should quietly vanish on a timer.

### the staleness reaper

an incident can get stuck active if charon misses its resolved edge, either because charon was down when grafana sent it, or because a dedup key drifted so the resolve did not match the firing. grafana re-sends *firing* every `repeat_interval` while a condition holds, so charon treats that as a heartbeat: an incident backed by a firing that is not re-asserted within a grace window (a few missed intervals, configurable) is auto-closed by a reaper, on the reading that the condition cleared and the resolved edge was lost. on boot, active incidents are marked unconfirmed until grafana re-asserts them; a still-firing condition re-asserts within one interval and stays, a resolved-while-down one never re-asserts and is reaped. this is what stops a missed resolve from leaving a ghost on the board forever.

the reaper only touches incidents with a firing heartbeat. one-shot events (sab, unbound) have no heartbeat and are never reaped; they wait for manual resolve, as above. the grace window is derived from grafana's `repeat_interval` (see replacing apprise), so the two are configured together.

## state, concurrency, and convergence

several things mutate an incident: an ingest firing or resolve, a button click, the snooze timer, the re-notification sweep, the reaper. left unsynchronised they race, a repeat-firing edit landing after a resolve delete (resurrecting a ghost), an old repost overwriting a newer message id, two sends for the same incident interleaving. two rules keep it correct.

first, mutations of a single incident are serialized. all writes for a given `DedupKey` run through one owner (a per-key worker, or a transaction guarded by a status-and-version precondition), so a transition always reads the current state and a stale writer loses. SQLite is a single writer under WAL, which charon leans into rather than fights: the incident row's `version` is the concurrency token, and a notifier job carrying an old version is dropped.

second, discord is driven by convergence, not by fire-and-forget calls. charon records the *desired* discord state for each incident in SQLite (a message present with a given rendered content hash, or absent), separate from the last confirmed state. a converger reconciles the two: it posts, edits, or deletes to make discord match the desired state, idempotently, and records the outcome. this is why a crash between "delete the discord message" and "stamp resolved" is recoverable: the desired state is already absent, so on boot the converger retries the delete and reaches the same end. it is also what bounds the outage backlog, the converger only ever needs to reach the latest desired state per incident, so superseded work collapses instead of piling up.

restart reconciliation is the converger running at boot against the persisted desired state, plus the reaper's unconfirmed-until-reasserted marking: a message that is gone is reposted, a pending delete is retried, a still-firing incident is confirmed by the next grafana heartbeat, a resolved-while-down one is reaped.

## rendering

each incident message is a Components V2 message (message flag `IS_COMPONENTS_V2`), not a classic embed. the structure is a Container with an accent colour set by severity, holding Text Displays for the title, body, host, and labels, a separator, and an action row of buttons.

- severity to accent: info blue, warning amber, critical red. an acknowledged incident is muted (greyed) so the board reads at a glance: bright is unattended, muted is handled.
- the buttons are acknowledge, snooze, and resolve, in one action row inside the container. each `custom_id` encodes the action and the incident id (`ack:<id>`, `snz:<id>`, `res:<id>`), kept well under the 100-character limit and re-validated against the store on receipt, never trusted as sent.

the V2 flag is sticky: once a message is posted with it, it cannot be removed on edit, so an incident is a V2 message from its first post through every edit and until it is deleted. all edits re-send the full component tree, which is fine because the tree is rebuilt from incident state each time.

## interactions

button clicks arrive over the gateway as `INTERACTION_CREATE` events; charon responds with an outbound HTTPS call to discord's interaction callback. no inbound public url is needed, which is what makes this work from the LAN behind no ingress. charon needs zero gateway intents to receive interactions; it runs unprivileged on the gateway.

- **acknowledge / resolve**: a discord interaction must be answered within 3 seconds, and that budget is not worth betting on under a locked SQLite writer, a busy sd card, or a queued discord call. so every interaction is deferred immediately (a deferred-update response), then the state transition runs against the store and the card is edited or deleted asynchronously through the paced queue. the transition is idempotent, so a double-click or a retried interaction does not double-apply.
- **snooze**: the button click answers with an ephemeral message (visible only to the clicker) carrying a string-select of durations (15m, 1h, 4h). picking one applies the snooze, edits the incident card to show "snoozed until T by X", and clears the ephemeral prompt. the picker is ephemeral so the channel stays clean and the transient dropdown state is not shared.

interaction tokens are valid for 15 minutes, which bounds only the ephemeral snooze exchange; the incident card itself is edited through normal channel message edits keyed by the stored message id, not the interaction token, so charon can edit or delete a card of any age.

## sending and rate limits

discord rate-limits message operations per channel (creation buckets at roughly five per five seconds) and globally (about fifty requests a second), and repeated 429s risk a day-long ip ban. every send (create, edit, delete, repost) goes through a per-channel paced queue: a token bucket with jitter holds the create rate under discord's per-channel limit, sends for a given incident stay ordered, and the disgo rest client's own bucket-aware 429 waiting sits underneath as a second layer. the queue coalesces superseded work for the same incident (an edit then a delete collapses to the delete; an edit then another edit keeps the latest), so a burst never enqueues obsolete sends. the backlog is bounded and its depth is a metric; while the discord path is degraded the queue holds the latest desired state per incident rather than a growing log of operations, and re-notification is paused so recovery does not itself become a storm. a couple dozen incidents clear in a few seconds; a real outage replays as a coalesced, paced catch-up, not a flood.

## data model

SQLite through the pure-go `modernc.org/sqlite` driver, so the build is cgo-free and the arm64 cross-compile stays clean. one file on a bind mount, WAL mode.

the core table is `incidents`:

- `dedup_key` (unique among active rows), `channel`, `severity`, `status` (`active` | `resolved`), `version` (the concurrency token)
- `title`, `body`, `host`, `link`, `labels` (json)
- `desired_present` and `content_hash` (the desired discord state the converger drives toward), plus `message_id` and `channel_id` (the last confirmed discord ids)
- `created_at`, `last_seen_firing`, `acked_at`, `acked_by`, `snoozed_until`, `last_notified_at`, `resolved_at`

active-incident lookup is by `dedup_key` where `status = active`. the snooze, re-notification, and reaper sweeps index `snoozed_until`, `last_notified_at`, and `last_seen_firing`. the converger reads rows whose confirmed discord state differs from the desired one. `busy_timeout` is set so a sweep and an interaction that contend wait rather than error, and every mutation is a transaction with a `version` precondition. closed incidents keep their rows for MTTR and for answering "did it resolve, or did charon die", which a deleted board message alone cannot answer.

the write load is a handful of rows per alert event, negligible against the prometheus tsdb already on the same sd card. it does not move the sd-wear compromise the alerting spec documents.

## deployment

- a static arm64 binary from a multi-stage build (go 1.24 builder, distroless or scratch final), cgo disabled.
- runs in triton's `monitoring` compose stack, in the slot apprise held. apprise, its config, and its vhost are removed (see replacing apprise).
- the ingest port is published to `192.168.20.10` (vlan) and `127.0.0.1` (loopback), matching apprise's reach, so grafana, the titan scripts, and unbound can all post.
- an outbound gateway websocket to discord (disgo auto-reconnects and resumes).
- the SQLite file on a `./data` bind mount under the stack directory.
- a `/metrics` endpoint scraped by prometheus, so a dead charon is itself an alert (subject to the triton-down gap: if triton is down, charon and prometheus are both down, the accepted SPOF).
- a caddy vhost `charon.rotted.io` is optional and not in v1; there is no web ui yet. `/metrics` and `/ingest` are bound to their hosts directly.

### configuration and secrets

- the bot token and the discord channel ids live in the gitignored `.env`, placeholders in the repo mirror, keepassxc the master. one bot, its token held only here.
- routing is a config file mapping each function tag to a channel id, many tags allowed to share one channel, plus a fallback channel for an unrecognised tag (routed and logged, never dropped):

```
channels:
  infra:     <infra channel id>
  network:   <infra channel id>     # same channel as infra to start
  media:     <media channel id>
  downloads: <media channel id>     # split later by pointing this elsewhere
  fallback:  <infra channel id>
```

- the re-notification interval and the snooze duration options are config with sensible defaults.

## failure modes and states

- **discord unreachable / gateway down**: ingest keeps accepting and recording incidents to SQLite; the converger cannot reach discord, so it holds the latest desired state per incident (bounded and coalesced, not a growing operation log) and re-notification pauses. disgo reconnects the gateway itself. buttons are dead during the outage but no incident state is lost, and on reconnect the converger drives discord back to the desired state as a paced catch-up. queue depth and the degraded state are metrics, not silent.
- **charon restart**: all state is in SQLite. on boot the converger reconciles each incident's confirmed discord state against its desired state (repost a missing message, retry a pending delete), and active incidents are marked unconfirmed until a grafana heartbeat re-asserts them; one that resolved while charon was down never re-asserts and the reaper closes it. this is why the desired state, the `version`, and `last_seen_firing` are persisted, not held in memory.
- **crash mid-resolve**: a crash between deleting the discord message and stamping the row is recoverable. the desired state is set to absent before the delete, so on boot the converger retries the delete and reaches the same end, no orphaned live message, no closed row with a ghost.
- **unknown channel tag**: routed to the fallback channel and logged, so a misconfigured emitter is loud, not silent.
- **adapter no-match**: 4xx and a log line. a controlled emitter sending a shape nothing claims is a bug to see.
- **resolved for an unknown or already-closed key**: no-op.
- **charon itself down**: the prometheus scrape gap raises a grafana alert, except under the triton-down SPOF where the whole stack is down together.

## testing

- adapter `Match` tests: a native payload parses to the right `Event`; a foreign payload returns `ErrNotMatched`.
- core lifecycle against a fake `Notifier` (the hexagonal payoff, no discord needed): firing creates and posts; a repeat firing edits and does not post a second; a resolved edge deletes; acknowledge stops re-notification; snooze suppresses then re-pings at the window; manual resolve deletes; the re-notification sweep reposts an unattended incident and leaves an acked or snoozed one alone.
- store round-trip and restart reconciliation: active incidents rebuild, a deleted-out-of-band message reposts, closed stays closed.
- send-queue ordering, coalescing, and 429 backoff against a fake discord client.
- the adversarial paths codex flagged: a resolved edge missed while charon is down is reaped after the grace window; a discord outage replays as a coalesced, paced catch-up rather than a flood; concurrent firing-edit, resolve-delete, and re-notify-repost for one `DedupKey` converge to a single correct end state with no resurrected ghost and no stale message id; a locked or slow SQLite writer does not blow the 3-second interaction budget because interactions defer first; an oversized post, or an unauthenticated one when a token is configured, is rejected.
- the arm64 build itself, in the Dockerfile.

behaviour and the invariants are the target, not branch count. the costly paths to protect are dedup (a duplicate storm), the delete-on-resolve board staying correct, and restart reconciliation, because those are where a regression is a channel full of ghosts or lost acknowledgements.

## replacing apprise

apprise is removed, not run in parallel. the change is one coordinated cutover, and the live deploy is gated on explicit sign-off, but there is no compatibility shim and no dual-run window.

removed:

- the apprise container, `apprise-config`, and the `apprise.rotted.io` caddy vhost and dns record, from `triton/monitoring`.

repointed to charon's event schema:

- the grafana contact point: the payload template is rewritten from the apprise json (`tag`/`title`/`body`) to the `Event` schema, its url pointed at charon's `/ingest`, and the alert rules gain a `severity` label. this is provisioned config, validated through the grafana api before it is written, because a bad alerting file stops grafana from starting (a documented gotcha).
- grafana's notification-policy `repeat_interval` becomes an explicit deployment invariant, not left at a default. it is set to a defined value and kept, because charon consumes those repeats as the firing heartbeat the reaper needs, and charon never re-notifies on a grafana repeat, only on its own timer. so grafana repeating and charon re-notifying cannot double-fire, and the reaper's grace window is derived from this interval.
- the titan scripts `notify-apprise.sh` and `sab-notify.sh`: rewritten to POST event json instead of apprise form data.
- the triton `apprise-notify@.service` unbound `OnFailure` unit: rewritten to POST event json.

unchanged: the truenas direct-to-discord alert (never went through apprise), dozzle, the one-webhook-per-app principle (charon holds the bot token and channel ids in the same gitignored `.env`, same discipline).

## designing for future correlation

grouping and threaded incidents were considered and cut from v1, but the seam is left deliberate so they do not force a rebuild:

- the `Event` carries `GroupKey`, unused now. a future correlation model groups incidents that share it.
- the incident core is the single owner of lifecycle, so a grouping layer sits above it (a group owns member incidents) without the discord side or the store schema changing shape underneath.
- the discord side already isolates rendering behind the `Notifier` port, so moving from one-message-per-incident to a thread-or-forum-per-group is a rendering and lifecycle change in one place, not a cross-cutting one.

this is a dormant touchpoint, not a built feature. nothing in v1 reads `GroupKey`, and the board is flat.

## verification

- a forced grafana alert (stop jellyfin) posts one incident message in the routed channel, coloured by severity, with the three buttons.
- the same rule firing again on the next interval edits that message, and does not post a second.
- clicking acknowledge greys the card and stops the re-notification sweep from reposting it; the incident stays on the board.
- clicking snooze shows the ephemeral duration picker; picking 15m marks the card snoozed and reposts it after the window if still firing.
- restarting jellyfin resolves the alert and the message is deleted; the closed incident is in the database.
- clicking resolve on a one-shot (a forced sab failure) deletes it from the board.
- a charon restart with active incidents reposts nothing already present, retries a pending delete, and reattaches the existing message ids.
- killing charon mid-resolve (after the discord delete, before the row is stamped) and restarting leaves no ghost message and no stuck-open row.
- an incident whose grafana firing stops while charon is down is reaped after the grace window rather than lingering active.
- a synchronised wave (force many probes down at once) drains through the paced queue without a 429, and re-notification stays paused until the backlog clears.
- prometheus scrapes charon's `/metrics`; stopping charon raises the charon-down alert (outside the triton SPOF).
- an unrecognised ingest payload returns 4xx and logs; an unknown tag routes to the fallback channel; an oversized or (when a token is set) unauthenticated post is rejected.

## knowledgebase impact

**project KB**: charon is a new standalone project with no existing KB owner. its knowledgebase is created at `rotten-division/charon` (via the KB bootstrap during implementation), owning charon's model, the `Event` contract, the adapter registry, the incident lifecycle, and the routing and deployment shape. the initial document set is part of this design, not a later pass.

**existing documents read for this design**: the homelab KB, `rotten-division/homelab`. the fleet telemetry and alerting spec `docs/specs/2026-07-01-fleet-telemetry-alerting-design.md` (grafana as source, discord as sink, apprise as the router being replaced, the function-tag routing, the triton SPOF and sd-card compromise), `overview.md`, `docs/hosts/triton.md` (the live monitoring stack and the apprise wiring), `docs/hosts/titan.md` (the arr/sab emitters), `docs/gotchas.md` (apprise stateful `.cfg` naming, grafana routes through apprise not direct, one-webhook-per-app, grafana 13 alerting provisioning crashes), and the live config under `triton/monitoring/` and `titan/apps/scripts/`.

**documents that constrain this design**: the alerting spec (the notification-routing section this implements, the discord-sink and one-webhook-per-app conventions, the SPOF and sd-card stances) and `preferences/design.md` (proportionate design, ownership, redesign-over-patch, migration).

**durable knowledge introduced**:

- charon's model and ownership: ingest turns requests into events via adapters, the core owns incident state, the store owns persistence, the discord side owns rendering and interactions.
- the `Event` contract and the `Adapter` interface as the two boundaries the core depends on, with deterministic path-based adapter routing (each adapter owns a path, no body-sniffing first-match).
- the invariants: one active incident per `DedupKey`; the channel shows only active incidents (delete on resolve, not edit-to-resolved); charon owns re-notification, not grafana, and never re-notifies on a grafana repeat.
- the concurrency and convergence model: mutations of an incident are serialized per `DedupKey` under a `version` precondition, and discord is driven by a converger toward a persisted desired state, which is what makes a mid-resolve crash and an outage backlog recoverable and bounded.
- the staleness reaper and the grafana-firing-as-heartbeat contract: a heartbeat-backed incident that stops re-firing is auto-closed after a grace window derived from grafana's `repeat_interval`; one-shot events have no heartbeat and are never reaped.
- the ingest trust boundary: the vlan is trusted (apprise parity), with a body-size cap and an optional static bearer token, and per-emitter signing a deliberate non-goal.
- re-notification is a paced, jittered, coalesced repost that pauses while degraded, so it cannot storm a channel or trip the 429 ban.
- routing is a config tag-to-channel map, many tags to a channel, with a fallback.
- SQLite is the sole durable state; closed incidents are retained for MTTR and for disambiguating a resolve from a crash.
- the disgo version pin and its reason (pre-1.0 breaking changes on minor versions), distinct from the docker no-pin standard.
- the dormant `GroupKey` seam for a future correlation model.

**documents to create, update, move, or delete**:

- create: the charon project KB (index, the system/model document, the routing and deployment notes).
- update at cutover, in the homelab KB (apprise is the running truth until then): `docs/hosts/triton.md` monitoring section (apprise to charon), the alerting spec's notification-routing and secrets sections (apprise router to the charon bot), `docs/gotchas.md` (replace the two apprise gotchas with charon's, keep one-webhook-per-app noting charon holds a bot token and channel ids), `overview.md` current-state, and the config mirrors under `triton/monitoring/` (remove the apprise service and `apprise-config`, add charon), `triton/network` caddy (remove the apprise vhost), the grafana `contactpoints.yml` and `rules.yml`, `titan/apps/scripts/notify-apprise.sh` and `sab-notify.sh`, and `triton/network/systemd/apprise-notify@.service`.

**source locations expected to be linked after implementation**: the charon repo (the core, the adapter registry, the discord side, the store) from the charon KB; the `triton/monitoring` charon service and the repointed emitters from the homelab KB.

**knowledge that stays local to this spec**: the rejected alternatives, kept here because they informed the choice but are not durable project knowledge. classic embeds (Components V2 gives the severity accent and inline buttons on one card); a forum-or-thread board (dropped with grouping; deleting a message orphans its thread, which was the reason forums looked necessary, and without grouping the point is moot); discordgo (disgo's interaction router and V2 builders fit the per-incident button model, at the cost of a smaller community and a pre-1.0 api); and a parallel run alongside apprise (cut, apprise is replaced outright).
