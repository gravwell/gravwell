# Local AWS Services

Local development environment for testing S3, SQS, and Kinesis ingesters using Docker containers that emulate AWS services.

## Prerequisite setup

Install both of these before starting:
- [aws cli v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- [Docker](https://docs.docker.com/engine/install/)

## Docker services

Each AWS service is emulated by a lightweight open-source alternative:
- [garage](https://garagehq.deuxfleurs.fr/) for S3
- [elasticmq](https://github.com/softwaremill/elasticmq) for SQS
- [kinesis-local](https://github.com/saidsef/aws-kinesis-local) for Kinesis

The compose file spins up a Gravwell instance alongside the three emulators. Gravwell exposes the web UI on `8080` and the ingester cleartext port on `4023`.

`docker-compose.yaml`:
```yaml
services:
  gravwell:
    image: ghcr.io/gravwell/alpha-next-minor:5.9.0-alpha20260512114933
    container_name: gravwell
    ports:
      - "8080:80"
      - "4023:4023"

  garage:
    image: dxflrs/garage:v2.3.0
    container_name: garage
    command: /garage server --single-node --default-bucket
    restart: unless-stopped
    environment:
      - GARAGE_DEFAULT_ACCESS_KEY=garage_access_key
      - GARAGE_DEFAULT_SECRET_KEY=garage_secret_key
      - GARAGE_DEFAULT_BUCKET=garage-bucket
    ports:
      - "3900:3900"
      - "3901:3901"
      - "3902:3902"
      - "3903:3903"
    volumes:
      - ./garage.toml:/etc/garage.toml
      - ./data:/var/lib/garage/data
      - ./meta:/var/lib/garage/meta

  elasticmq:
    image: softwaremill/elasticmq:1.7.1
    container_name: elasticmq
    restart: unless-stopped
    ports:
      - "9324:9324"
    volumes:
      - ./elasticmq.conf:/opt/elasticmq.conf

  kinesis-local:
    image: saidsef/aws-kinesis-local:v2026.04
    container_name: kinesis-local
    restart: unless-stopped
    ports:
      - "4567:4567"
```

### Service configs

ElasticMQ needs a config file to define queues. The `test` queue is the one we'll use; the others are examples of dead-letter and FIFO setups.

`elasticmq.conf`:

```conf
include classpath("application.conf")

node-address {
  port = 9324
}

rest-sqs {
  bind-port = 9324
}

messages-storage {
  enabled = false
}

auto-create-queues {
  enabled = true
  template {
    defaultVisibilityTimeout = 30 seconds
    tags {
        type = "dynamic"
    }
  }
}

queues {
  test { }

  01-simple-queue { }

  02-queue-with-dead-letter {
    defaultVisibilityTimeout = 10 seconds
    delay = 5 seconds
    receiveMessageWait = 0 seconds
    deadLettersQueue {
      name = "03-dead-letter-queue"
      maxReceiveCount = 3
    }
  }

  03-dead-letter-queue { }

  04-fifo-queue {
    fifo = true
    contentBasedDeduplication = true
  }
}
```

Garage requires a TOML config for its S3-compatible API, RPC, web, and admin endpoints. The `rpc_secret` is just a local dev value — not sensitive.

`garage.toml`:

```toml
metadata_dir = "/tmp/meta"
data_dir = "/tmp/data"
db_engine = "sqlite"

replication_factor = 1

rpc_bind_addr = "[::]:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret = "e7f403f2e0cdc60e3c001429af3752dbbd318bb19558553400e079eec1793ee2"

[s3_api]
s3_region = "garage"
api_bind_addr = "[::]:3900"
root_domain = ".s3.garage.localhost"

[s3_web]
bind_addr = "[::]:3902"
root_domain = ".web.garage.localhost"
index = "index.html"

[admin]
api_bind_addr = "[::]:3903"
admin_token = "ir91f2VJYbwDyJE7sLmMhhv4vApuPts6yl4mFJsZTXI="
metrics_token = "JgnyNCtj4FkYOKZoDmw5Bdv2KQHhPxQ5/xYiDTQvH10="
```

### AWS CLI profiles

Set up named profiles so the AWS CLI knows how to reach each local service. Each profile points at the corresponding container's endpoint.

`~/.aws/config`:

```ini
[profile garage]
endpoint_url = http://localhost:3900
region = garage
output = json

[profile elasticmq]
endpoint_url = http://localhost:9324
region = us-east-1
output = json

[profile kinesis-local]
endpoint_url = http://localhost:4567
region = us-east-1
output = json
```

Credentials are hardcoded dev values matching what the containers expect.

`~/.aws/credentials`:

```ini
[garage]
aws_access_key_id = garage_access_key 
aws_secret_access_key = garage_secret_key

[elasticmq]
aws_access_key_id = elasticmq
aws_secret_access_key = elasticmq

[kinesis-local]
aws_access_key_id = kinesis_local
aws_secret_access_key = kinesis_local
```

## Bring it up

Start everything:

```bash
$ docker compose up --build -d
```

### Verify connectivity

Run a quick list command against each service to confirm the CLI can talk to the containers:

```bash
$ aws --profile garage s3api list-buckets

{
    "Buckets": [
        {
            "Name": "garage-bucket",
            "CreationDate": "2026-05-12T17:03:44.218000+00:00"
        }
    ],
    "Owner": {
        "DisplayName": "default access key",
        "ID": "garage_access_key"
    },
    "Prefix": null
}
```

```bash
$ aws --profile elasticmq sqs list-queues

{ 
    "QueueUrls": [] 
}
```

```bash
$ aws --profile kinesis-local kinesis list-streams

{
    "StreamNames": []
}
```

### Create test resources

Garage already provisions a default S3 bucket via its environment variables. SQS and Kinesis need their resources created manually:

```bash
$ aws --profile elasticmq sqs create-queue --queue-name test --region us-east-1
{
    "QueueUrl": "http://localhost:9324/000000000000/test"
}
```

```bash
$ aws --profile kinesis-local kinesis create-stream --stream-name test --region us-east-1 --shard-count 1

$ aws --profile kinesis-local kinesis list-streams
{
    "StreamNames": [
        "test"
    ]
}
```

## Ingester configs

Each ingester runs outside Docker and connects to Gravwell on `localhost:4023`. The key settings are the local endpoints, static credentials, and `S3-Force-Path-Style=true` for Garage (it doesn't support virtual-hosted-style buckets).

`s3.conf`:

```ini
[Global]
Ingester-UUID=4c9143f8-be73-4c4a-8fcc-ed05d8ce8fd0
Ingest-Secret=IngestSecrets
Cleartext-Backend-Target=localhost:4023
Log-File=/tmp/s3.log
State-Store-Location=/tmp/s3.state
Worker-Pool-Size=10
Connection-Timeout=10s

[Bucket "test"]
  Endpoint=http://localhost:3900
	Region=garage
	ID=garage_access_key
	Secret=garage_secret_key
	Bucket-Name=garage-bucket
	Tag-Name=s3
	Credentials-Type=static
	S3-Force-Path-Style=true
```

The SQS ingester polls the queue URL directly. No state store is needed here since SQS handles message visibility itself.

`sqs.conf`:

```ini
[Global]
Ingester-UUID=4ea33733-19dd-4a21-92ed-172aceb0f9a5
Ingest-Secret=IngestSecrets
Cleartext-Backend-Target=localhost:4023
Log-File=/tmp/sqs.log

[Queue "test"]
	Region=us-east-1
	Endpoint=http://localhost:9324
	Queue-URL=http://localhost:9324/000000000000/test
	Tag-Name=sqs
	AKID=elasticmq
	Secret=elasticmq
	Credentials-Type=static
```

The Kinesis ingester uses `TRIM_HORIZON` to read from the beginning of the stream. It tracks its position via `State-Store-Location` so it survives restarts without re-reading.

`kinesis.conf`:

```ini
[Global]
Ingester-UUID=b9757493-c5cf-4eb7-86d1-14ad95589066
Ingest-Secret=IngestSecrets
Connection-Timeout=10s
Insecure-Skip-TLS-Verify = false
Cleartext-Backend-Target=localhost:4023
Log-Level=INFO
Log-File=/tmp/kinesis.log
State-Store-Location=/tmp/kinesis_ingest.state

AWS-Access-Key-ID=kinesis_local
AWS-Secret-Access-Key=kinesis_local

[KinesisStream "testStream"]
  Endpoint=http://localhost:4567
	Region=us-east-1
	Tag-Name=kinesis
	Stream-Name=test
	Iterator-Type=TRIM_HORIZON
	Parse-Time=false
```

## Generate data

The `genawsdata` tool pushes synthetic events into all three services at once. Adjust `-num-events` as needed.

```bash
$ go run ./cmd/genawsdata/ -v \
	-s3-profile garage -s3-endpoint http://localhost:3900 \
	-s3-buckets garage-bucket -sqs-endpoint http://localhost:9324 \
	-sqs-queues http://localhost:9324/000000000000/test -sqs-profile elasticmq \
	-kinesis-profile kinesis-local -kinesis-endpoint http://localhost:4567 \
	-kinesis-streams test \
	-num-events 100
```
