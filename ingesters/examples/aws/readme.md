# Local AWS Services

Local development environment for testing S3, SQS, and Kinesis ingesters using Docker containers that emulate AWS services.

## Prerequisite setup

Install both of these before starting:
- [aws cli v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- [Docker](https://docs.docker.com/engine/install/)

## Docker services

All three AWS services (S3, SQS, Kinesis) are emulated by a single lightweight Docker container
- [kumo](https://github.com/sivchari/kumo)

The compose file spins up a Gravwell instance alongside the AWS emulator. Gravwell exposes the web UI on `8080` and the ingester cleartext port on `4023`. Kumo will be exposed on port `4566`.

`docker-compose.yaml`:
```yaml
services:
  gravwell:
    image: gravwell/gravwell:latest
    container_name: gravwell
    ports:
      - "8080:80"
      - "4023:4023"

  kumo:
    image: ghcr.io/sivchari/kumo@sha256:e63054fbe10eb17b0c9142e937e11b3f4ee2709ac1c80035f3220542f3e5b045 # v0.26.0
    container_name: kumo
    ports:
      - "4566:4566"
    environment:
      - "KUMO_DATA_DIR=/data"
    volumes:
      - kumo_data:/data

volumes:
  kumo_data:
```

### AWS CLI profile

Set up a named profile so the AWS CLI knows how to reach Kumo.

`~/.aws/config`:

```ini
[profile kumo]
endpoint_url = http://localhost:4566
region = us-east-1
output = json
```

Credentials are hardcoded dev values — Kumo doesn't validate them, but the AWS CLI still requires something be set.

`~/.aws/credentials`:

```ini
[kumo]
aws_access_key_id = kumo_access_key
aws_secret_access_key = kumo_secret_key
```

## Bring it up

Start everything:

```bash
$ docker compose up --build -d
```

### Create test resources

Kumo starts empty, so the S3 bucket, SQS queue, and Kinesis stream all need to be created manually:

```bash
$ aws --profile kumo s3 mb s3://test

$ aws --profile kumo sqs create-queue --queue-name test
{
    "QueueUrl": "http://localhost:4566/000000000000/test"
}

$ aws --profile kumo kinesis create-stream --stream-name test --shard-count 1
```

### Verify connectivity

Run a quick list command against each service to confirm the CLI can talk to the container:

```bash
$ aws --profile kumo s3api list-buckets

{
    "Buckets": [
        {
            "Name": "test",
            "CreationDate": "2026-05-12T17:03:44.218000+00:00"
        }
    ],
    "Owner": {
        "DisplayName": "default access key",
        "ID": "kumo_access_key"
    },
    "Prefix": null
}
```

```bash
$ aws --profile kumo sqs list-queues

{
    "QueueUrls": [
        "http://localhost:4566/000000000000/test"
    ]
}
```

```bash
$ aws --profile kumo kinesis list-streams

{
    "StreamNames": [
        "test"
    ]
}
```

## Ingester configs

Each ingester runs outside Docker and connects to Gravwell on `localhost:4023`. All three point at the same Kumo endpoint (`localhost:4566`) and use the same static credentials. `S3-Force-Path-Style=true` is set for the S3 ingester since Kumo doesn't support virtual-hosted-style buckets.

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
    Endpoint=http://localhost:4566
	Region=us-east-1
	ID=kumo_access_key
	Secret=kumo_secret_key
	Bucket-Name=test
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
	Endpoint=http://localhost:4566
	Queue-URL=http://localhost:4566/000000000000/test
	Tag-Name=sqs
	AKID=kumo_access_key
	Secret=kumo_secret_key
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

AWS-Access-Key-ID=kumo_access_key
AWS-Secret-Access-Key=kumo_secret_key

[KinesisStream "testStream"]
    Endpoint=http://localhost:4566
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
	-s3-profile kumo -s3-endpoint http://localhost:4566 \
	-s3-buckets test -sqs-endpoint http://localhost:4566 \
	-sqs-queues http://localhost:4566/000000000000/test -sqs-profile kumo \
	-kinesis-profile kumo -kinesis-endpoint http://localhost:4566 \
	-kinesis-streams test \
	-num-events 100
```
