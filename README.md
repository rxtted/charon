# charon

a small self-hosted discord bot that turns monitoring alerts into a live incident board. one card per incident, tracks firing to resolved, ack/snooze/resolve from buttons, and the card disappears when the alert clears so the channel only shows what's currently on fire.

i wrote this after trying a few of the off-the-shelf routers and every one missed something i wanted. apprise, the grafana-native discord push and the various bridges can't render a real embed (thumbnail, footer), let alone do buttons. Novu does the lot but it's a six-container platform i'd be babysitting. Notifiarr's embeds are great but its bot runs on their cloud, not mine. and the stateless ones just forward the same grafana rule every time it re-fires with no idea when it cleared, so the channel fills with duplicates.

what i actually wanted was rich embeds, buttons, and everything self-hosted under my own control, without standing up a whole platform for it. a small go bot was the only thing that did all of it at once.

this project is definitely tailored to my use case, but if you see something you like, fork it or take whatever's useful. issues welcome too if something's broken.
