# MQTT Ingester

Subscribes to one or more MQTT brokers and ingests every message it receives.

The payload is stored as the entry body, byte for byte. The MQTT framing
(topic, QoS, retain flag, content type and any v5 user properties) is attached
as enumerated values, so a binary or already-JSON payload is never wrapped or
re-encoded.

## MQTT version

This ingester speaks **MQTT version 5 only**. It will not connect to a broker
limited to MQTT 3.1.1 — notably Mosquitto 1.x and Azure IoT Hub. Mosquitto 2.x,
EMQX, HiveMQ, NanoMQ, VerneMQ and AWS IoT Core all support v5.

## Quick start

```ini
[Subscription "sensors"]
	Broker="mqtt://127.0.0.1:1883"
	Topic="factory/#"
	Default-Tag=mqtt
```

See `mqtt.conf` for the full set of options.

## Delivery guarantees

Messages are acknowledged only after they have been handed to the ingest muxer,
so a crash or a broker disconnect results in redelivery rather than loss. This
is at-least-once: a message that was written but not yet acknowledged when the
ingester stopped will arrive again on the next connection.

Two things are needed for that to hold across restarts:

- **A stable `Client-ID`.** One is derived from the ingester UUID and the
  subscription name if you do not set it. Changing it discards the broker-side
  session and anything queued in it.
- **`QoS=1` or `QoS=2`.** QoS 0 has no acknowledgement and therefore no
  guarantee at all; it also has no flow control, so a fast publisher can
  outrun the indexer. QoS 1 is the default.

`Session-Expiry` controls how long the broker holds the session, and therefore
how long the ingester can be down before queued messages are discarded.

### Failure handling

A write that fails transiently — the indexer being unreachable, for example —
is retried with backoff until it succeeds or the ingester shuts down. It is
not skipped, and it is not acknowledged until it lands.

This matters more than it looks: MQTT acknowledgements are processed in arrival
order, so a single message that is never acknowledged blocks every
acknowledgement behind it. Once `Max-In-Flight` messages have accumulated the
broker stops delivering entirely. Retrying rather than giving up is what keeps
the subscription alive through an outage.

`Synchronous=true` goes further and syncs to the indexer before acknowledging
each message. Be aware of the cost: a sync always sleeps at least 10ms, and
MQTT delivers one message at a time with no batch to amortize it over, so a
synchronous subscription is capped at roughly 100 messages/second no matter how
fast the broker and indexer are. Leave it off unless losing an in-flight
message on a crash is worse than the throughput ceiling.

The exception is an entry that can never be written — one larger than the
indexer's maximum entry size, or carrying a tag that was never negotiated.
Retrying those forever would wedge the subscription on a single message, so
they are logged at error level and dropped.

## Scaling out

`Shared-Group` subscribes via `$share/<group>/<topic>`, which makes the broker
load balance deliveries across every ingester using the same group. Run several
ingesters with the same `Shared-Group` and different `Client-ID`s to split a
firehose.

## Backpressure

`Max-In-Flight` is sent to the broker as the MQTT Receive Maximum. It caps how
many unacknowledged messages the broker may have outstanding, which is what
stops a fast broker from outrunning the indexer. It also bounds the client's
internal receive buffer.

## Tag routing

`Default-Tag` is the tag for everything on a subscription. `Topic-Route` sends
matching topics somewhere else:

```ini
	Topic-Route="factory/line1/*:mqttline1"
	Topic-Route="factory/*/heartbeat:"
```

The match is a **glob, not an MQTT filter** — `*` does not cross a `/`
boundary, so `factory/line1/*` matches `factory/line1/temp` but not
`factory/line1/sub/temp`. The first matching route wins, and a route with an
empty tag drops the message (it is still acknowledged, so the broker does not
redeliver it).

## Timestamps

MQTT carries no publish timestamp, so entries are stamped on arrival. Set
`Extract-Timestamps=true` to search the payload for one instead, with
`Assume-Local-Timezone`, `Timezone-Override` and `Timestamp-Format-Override`
available to steer the parse. `Ignore-Timestamps=true` forces arrival time.
