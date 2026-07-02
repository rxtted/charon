# charon

a small discord bot that turns monitoring alerts into a live incident board. it ingests alert events over http, tracks each condition as one incident (dedup, firing to resolved, acknowledge/snooze/resolve from buttons), and keeps the channel showing only what is currently on fire, deleting a card when its incident resolves.

it exists to replace a stateless webhook fan-out. a webhook is send-only, so it can't dedup a rule that fires every interval, can't tell you an alert cleared, and can't take a button click. a bot holds state, so it can.

## how it works

- **ingest**: an http endpoint turns alert events into one internal `Event` type through a small self-registering adapter set. grafana's contact point and the shell emitters post the event json directly; a rigid third-party webhook gets its own adapter.
- **core**: an incident state machine on sqlite. one active incident per dedup key, mutations of a key serialized through a shared lock, closed incidents kept for history.
- **discord**: each incident renders as a components v2 message with a severity accent and ack/snooze/resolve buttons. a converger drives discord toward a desired state persisted in sqlite, so a restart or a discord outage reconciles instead of drifting. button clicks arrive over the gateway; no inbound url is needed.

## build

    CGO_ENABLED=0 go build ./cmd/charon

needs go 1.25, the floor the sqlite driver sets.

## image

published to ghcr for `linux/amd64` and `linux/arm64`: `ghcr.io/rxtted/charon:latest` tracks releases, `:nightly` tracks `main`, and each release is also tagged `:vX.Y.Z`. the compose in `deploy/` pulls `:latest`. to build it yourself instead:

    docker build -t charon .

## run

charon needs a discord bot token and a tag-to-channel map. fill in the examples:

    cp .env.example .env          # bot token, optional ingest token
    cp config.example.yml config.yml   # which alert tag posts to which channel

point `CHARON_CONFIG` at the config file and run the binary, or use the compose service in `deploy/`. it takes alert posts on `:8000` and serves prometheus metrics on `:9095`; restrict the ingest port to a trusted network at deploy time.

## develop

    go test ./...

the tests cover the adapters, the incident lifecycle, the send-and-converge path, and the store against a fake discord, so none of them need a live bot.
