# Local Azure Event Hubs

Local development environment for testing the AzureEventHubs ingester using Docker containers that emulate Azure Event Hubs.

## Prerequisite setup

Install before starting:
- [Docker](https://docs.docker.com/engine/install/)

## Docker services

Event Hubs is emulated using Microsoft's own official emulator, rather than a third-party alternative like the AWS example uses:
- [`mcr.microsoft.com/azure-messaging/eventhubs-emulator`](https://learn.microsoft.com/en-us/azure/event-hubs/overview-emulator) — the Event Hubs emulator itself.
- [`mcr.microsoft.com/azure-storage/azurite`](https://learn.microsoft.com/en-us/azure/storage/common/storage-use-azurite) — the emulator's required metadata/blob storage backend. The emulator will not start without it.

Starting the emulator container means accepting Microsoft's Software License Terms for it (`ACCEPT_EULA=Y` below) — see the [EULA](https://github.com/Azure/azure-event-hubs-emulator-installer/blob/main/EMULATOR_EULA.md) before running this.

The compose file spins up a Gravwell instance alongside the emulator and its storage backend. Gravwell exposes the web UI on `8080` and the ingester cleartext port on `4023`.

`docker-compose.yaml`:
```yaml
services:
  gravwell:
    image: gravwell/gravwell:latest
    container_name: gravwell
    ports:
      - "8080:80"
      - "4023:4023"

  emulator:
    container_name: eventhubs-emulator
    image: mcr.microsoft.com/azure-messaging/eventhubs-emulator:latest
    volumes:
      - ./eventhubs-config.json:/Eventhubs_Emulator/ConfigFiles/Config.json
    ports:
      - "5672:5672"
      - "9092:9092"
      - "5300:5300"
    environment:
      BLOB_SERVER: azurite
      METADATA_SERVER: azurite
      ACCEPT_EULA: "Y"
    depends_on:
      - azurite

  azurite:
    container_name: azurite
    image: mcr.microsoft.com/azure-storage/azurite:latest
    ports:
      - "10000:10000"
      - "10001:10001"
      - "10002:10002"
```

### Service config

The emulator needs a config file describing its namespace and event hub entities. `test` is the hub we'll use, with a `cg1` consumer group (the emulator also always creates a `$Default` group).

`eventhubs-config.json`:

```json
{
    "UserConfig": {
        "NamespaceConfig": [
            {
                "Type": "EventHub",
                "Name": "emulatorNs1",
                "Entities": [
                    {
                        "Name": "test",
                        "PartitionCount": "2",
                        "ConsumerGroups": [ { "Name": "cg1" } ]
                    }
                ]
            }
        ],
        "LoggingConfig": { "Type": "File" }
    }
}
```

## Bring it up

Start everything:

```bash
$ docker compose up --build -d
```

The emulator takes a few seconds to come up — it retries its startup health check internally until Azurite is ready, so don't be alarmed by an initial `Emulator Start up probe Unsuccessful` line in its logs. Once ready, `docker logs eventhubs-emulator` shows a line like:

```
Emulator Service is Successfully Up! ; Use connection string: "Endpoint=sb://localhost;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true;".
```

### Verify connectivity

Unlike AWS, there's no CLI tool that can list Event Hubs entities against the emulator (the Azure CLI's `eventhubs` commands only talk to real Azure resource manager, not the emulator's data plane). The emulator does expose an HTTP health check, which is a reasonable connectivity smoke test:

```bash
$ curl -s -o /dev/null -w "%{http_code}\n" http://localhost:5300/health

200
```

Beyond that, the real verification is ingesting data end to end: run the `genazureeventhubsdata` tool below and confirm the ingester's logs show it receiving and forwarding events.

## Ingester config

The ingester runs outside Docker and connects to Gravwell on `localhost:4023`. The key setting is `Event-Hubs-Endpoint`: when set, it tells the ingester to build an emulator-style connection string (`UseDevelopmentEmulator=true`) instead of connecting to a real `<namespace>.servicebus.windows.net` endpoint, so `Event-Hubs-Namespace` can be omitted entirely.

`azureeventhubs.conf`:

```ini
[Global]
Ingester-UUID=6f6e6b3a-6b5b-4b3e-9c7a-6a6b6f6e6b3a
Ingest-Secret=IngestSecrets
Cleartext-Backend-Target=localhost:4023
Log-File=/tmp/azureeventhubs.log
State-Store-Location=/tmp/azureeventhubs.state

[EventHub "test"]
	# Event-Hubs-Namespace is ignored (and can be omitted) whenever
	# Event-Hubs-Endpoint is set, since the emulator doesn't live under a real
	# Azure namespace.
	Event-Hubs-Endpoint=localhost
	Event-Hub=test
	Consumer-Group=cg1
	Token-Name=RootManageSharedAccessKey
	Token-Key=emulatorSasKeyNotForProduction==
	Initial-Checkpoint=start
	Tag-Name=azureeventhubs
	Parse-Time=false
```

The emulator's fixed SAS policy name is `RootManageSharedAccessKey`; the key itself isn't cryptographically validated by the emulator, so any non-empty value works — `Token-Key` above is an obviously-fake dev value, not a real secret.

## Generate data

The `genazureeventhubsdata` tool batches synthetic events and sends them to the emulator over AMQP. Adjust `-num-events` as needed.

```bash
$ go run ./cmd/genazureeventhubsdata -v \
	-endpoint localhost \
	-token-name RootManageSharedAccessKey \
	-token-key emulatorSasKeyNotForProduction== \
	-event-hubs test \
	-num-events 100
```
