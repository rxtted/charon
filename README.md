# charon

a small self-hosted discord bot that turns monitoring alerts into a live incident board. one card per incident, tracks firing to resolved, ack/snooze/resolve from buttons, and the card disappears when the alert clears so the channel only shows what's currently on fire.

i wrote this after trying a few of the existing notification routers, but they're all stateless webhook fan-outs. they'll happily forward the same grafana rule every time it re-fires and they've got no idea when it actually cleared, so the channel just fills up with duplicates. a bot holds state, so it can dedup and clean up after itself.

this project is definitely tailored to my use case, but if you see something you like, fork it or take whatever's useful. issues welcome too if something's broken.
