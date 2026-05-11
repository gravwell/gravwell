# OpenTelemetry Handler for Gravwell HTTP Ingester

## Overview

The HTTP Ingester now includes native OpenTelemetry (OTel) handlers that accept metrics and logs data from official OpenTelemetry clients using the OpenTelemetry Protocol (OTLP). This allows you to ingest telemetry data without modifying upstream OpenTelemetry clients.

## Features

### Metrics Handler

- **Native OTLP Support**: Accepts metrics in the standard OTLP format (protobuf and JSON)
- **Official OpenTelemetry SDKs**: Uses official OpenTelemetry protocol buffers
- **All Metric Types Supported**:
  - Gauge
  - Sum (with monotonicity and aggregation temporality)
  - Histogram
  - ExponentialHistogram
  - Summary
- **Automatic Type Conversion**: Metrics are decoded into native Go types and converted to JSON for storage
- **Resource Attributes**: Resource attributes are automatically attached as enumerated values to entries
- **Timestamp Handling**: Supports OTLP timestamps with configurable override options
- **Multiple Content Types**: Accepts both `application/x-protobuf` and `application/json` content types

### Logs Handler

- **Native OTLP Logs Support**: Accepts logs in the standard OTLP format (protobuf and JSON)
- **Official OpenTelemetry SDKs**: Uses official OpenTelemetry protocol buffers
- **Log Body Extraction**: The log body is extracted and stored as entry data
- **Severity Levels**: Captures severity number and severity text
- **Trace Context**: Preserves trace ID and span ID for correlation
- **Log Attributes**: Log attributes are attached as enumerated values
- **Resource Attributes**: Resource attributes are automatically attached as enumerated values
- **Timestamp Handling**: Supports both log timestamp and observed timestamp
- **Multiple Content Types**: Accepts both `application/x-protobuf` and `application/json` content types

## Configuration

### Metrics Listener Configuration

```ini
[OpenTelemetry-Metrics-Listener "otel-metrics"]
    URL="/v1/metrics"                # Standard OTLP endpoint
    Tag-Name="otel-metrics"          # Tag for ingested metrics
    Ignore-Timestamps=false          # Use timestamps from OTLP
    Encode-As-JSON=false             # Store metrics as JSON
    Debug-Posts=true                 # Enable debug logging
```

#### Metrics Listener with Authentication

```ini
# Basic Authentication
[OpenTelemetry-Metrics-Listener "otel-metrics-basic"]
    URL="/v1/metrics"
    Tag-Name="otel-metrics"
    AuthType=basic
    Username=metricuser
    Password=metricpass

# Preshared Token Authentication (Authorization header)
[OpenTelemetry-Metrics-Listener "otel-metrics-token"]
    URL="/v1/metrics"
    Tag-Name="otel-metrics"
    AuthType=preshared-token
    TokenName=Bearer
    TokenValue=your-secret-token-here

# Preshared Header Authentication (custom header)
[OpenTelemetry-Metrics-Listener "otel-metrics-header"]
    URL="/v1/metrics"
    Tag-Name="otel-metrics"
    AuthType=preshared-header
    TokenName=X-Custom-Auth
    TokenValue=your-secret-value
```

#### Metrics Configuration Options

- **URL** (string, optional): The HTTP endpoint path. Default: `/v1/metrics`
- **Tag-Name** (string, required): The Gravwell tag for ingested metrics
- **Ignore-Timestamps** (bool, optional): If true, use current time instead of OTLP timestamps. Default: `false`
- **Encode-As-JSON** (bool, optional): If true, encode metrics as JSON. Default: `false`
- **Debug-Posts** (bool, optional): Enable detailed logging of requests. Default: `false`
- **Preprocessor** ([]string, optional): List of preprocessors to apply to entries
- **AuthType** (string, optional): Authentication type. Options: `none`, `basic`, `jwt`, `cookie`, `preshared-token`, `preshared-parameter`, `preshared-header`. Default: `none`
- **Username** (string, optional): Username for `basic`, `jwt`, or `cookie` authentication
- **Password** (string, optional): Password for `basic`, `jwt`, or `cookie` authentication
- **LoginURL** (string, optional): Login endpoint URL for `jwt` or `cookie` authentication
- **TokenName** (string, optional): Token name for preshared authentication. Default: `Bearer` for `preshared-token`
- **TokenValue** (string, optional): Token value for preshared authentication

### Logs Listener Configuration

```ini
[OpenTelemetry-Logs-Listener "otel-logs"]
    URL="/v1/logs"                   # Standard OTLP endpoint
    Tag-Name="otel-logs"             # Tag for ingested logs
    Ignore-Timestamps=false          # Use timestamps from OTLP
    Disable-EVs=false                # Disable enumerated values
    Debug-Posts=true                 # Enable debug logging
```

#### Logs Listener with Authentication

```ini
# Basic Authentication
[OpenTelemetry-Logs-Listener "otel-logs-basic"]
    URL="/v1/logs"
    Tag-Name="otel-logs"
    AuthType=basic
    Username=loguser
    Password=logpass

# Preshared Token Authentication (Authorization header)
[OpenTelemetry-Logs-Listener "otel-logs-token"]
    URL="/v1/logs"
    Tag-Name="otel-logs"
    AuthType=preshared-token
    TokenName=Bearer
    TokenValue=your-secret-token-here

# Preshared Header Authentication (custom header)
[OpenTelemetry-Logs-Listener "otel-logs-header"]
    URL="/v1/logs"
    Tag-Name="otel-logs"
    AuthType=preshared-header
    TokenName=X-Custom-Auth
    TokenValue=your-secret-value
```

#### Logs Configuration Options

- **URL** (string, optional): The HTTP endpoint path. Default: `/v1/logs`
- **Tag-Name** (string, required): The Gravwell tag for ingested logs
- **Ignore-Timestamps** (bool, optional): If true, use current time instead of OTLP timestamps. Default: `false`
- **Disable-EVs** (bool, optional): If true, do not extract enumerated values from log attributes. Default: `false`
- **Debug-Posts** (bool, optional): Enable detailed logging of requests. Default: `false`
- **Preprocessor** ([]string, optional): List of preprocessors to apply to entries
- **AuthType** (string, optional): Authentication type. Options: `none`, `basic`, `jwt`, `cookie`, `preshared-token`, `preshared-parameter`, `preshared-header`. Default: `none`
- **Username** (string, optional): Username for `basic`, `jwt`, or `cookie` authentication
- **Password** (string, optional): Password for `basic`, `jwt`, or `cookie` authentication
- **LoginURL** (string, optional): Login endpoint URL for `jwt` or `cookie` authentication
- **TokenName** (string, optional): Token name for preshared authentication. Default: `Bearer` for `preshared-token`
- **TokenValue** (string, optional): Token value for preshared authentication

### Multiple Listeners

You can configure multiple OpenTelemetry listeners on different URLs:

```ini
[OpenTelemetry-Metrics-Listener "production"]
    URL="/v1/metrics"
    Tag-Name="otel-prod"

[OpenTelemetry-Metrics-Listener "development"]
    URL="/dev/v1/metrics"
    Tag-Name="otel-dev"

[OpenTelemetry-Logs-Listener "production-logs"]
    URL="/v1/logs"
    Tag-Name="otel-logs-prod"

[OpenTelemetry-Logs-Listener "development-logs"]
    URL="/dev/v1/logs"
    Tag-Name="otel-logs-dev"
```

## Usage

### Configuring OpenTelemetry Clients for Metrics

Configure your OpenTelemetry SDK to export metrics to the Gravwell HTTP ingester:

#### Go Example - Metrics

```go
import (
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Without authentication
exporter, err := otlpmetrichttp.New(context.Background(),
    otlpmetrichttp.WithEndpoint("gravwell-host:8080"),
    otlpmetrichttp.WithInsecure(),
    otlpmetrichttp.WithURLPath("/v1/metrics"),
)

// With Basic Authentication
exporter, err := otlpmetrichttp.New(context.Background(),
    otlpmetrichttp.WithEndpoint("gravwell-host:8080"),
    otlpmetrichttp.WithInsecure(),
    otlpmetrichttp.WithURLPath("/v1/metrics"),
    otlpmetrichttp.WithHeaders(map[string]string{
        "Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("username:password")),
    }),
)

// With Token Authentication
exporter, err := otlpmetrichttp.New(context.Background(),
    otlpmetrichttp.WithEndpoint("gravwell-host:8080"),
    otlpmetrichttp.WithInsecure(),
    otlpmetrichttp.WithURLPath("/v1/metrics"),
    otlpmetrichttp.WithHeaders(map[string]string{
        "Authorization": "Bearer your-secret-token-here",
    }),
)

provider := sdkmetric.NewMeterProvider(
    sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
)
```

#### Python Example - Metrics

```python
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
import base64

# Without authentication
exporter = OTLPMetricExporter(
    endpoint="http://gravwell-host:8080/v1/metrics"
)

# With Basic Authentication
credentials = base64.b64encode(b"username:password").decode("utf-8")
exporter = OTLPMetricExporter(
    endpoint="http://gravwell-host:8080/v1/metrics",
    headers={"Authorization": f"Basic {credentials}"}
)

# With Token Authentication
exporter = OTLPMetricExporter(
    endpoint="http://gravwell-host:8080/v1/metrics",
    headers={"Authorization": "Bearer your-secret-token-here"}
)

reader = PeriodicExportingMetricReader(exporter)
provider = MeterProvider(metric_readers=[reader])
```

### Configuring OpenTelemetry Clients for Logs

Configure your OpenTelemetry SDK to export logs to the Gravwell HTTP ingester:

#### Go Example - Logs

```go
import (
    "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
    sdklog "go.opentelemetry.io/otel/sdk/log"
)

// Without authentication
exporter, err := otlploghttp.New(context.Background(),
    otlploghttp.WithEndpoint("gravwell-host:8080"),
    otlploghttp.WithInsecure(),
    otlploghttp.WithURLPath("/v1/logs"),
)

// With Basic Authentication
exporter, err := otlploghttp.New(context.Background(),
    otlploghttp.WithEndpoint("gravwell-host:8080"),
    otlploghttp.WithInsecure(),
    otlploghttp.WithURLPath("/v1/logs"),
    otlploghttp.WithHeaders(map[string]string{
        "Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("username:password")),
    }),
)

// With Token Authentication
exporter, err := otlploghttp.New(context.Background(),
    otlploghttp.WithEndpoint("gravwell-host:8080"),
    otlploghttp.WithInsecure(),
    otlploghttp.WithURLPath("/v1/logs"),
    otlploghttp.WithHeaders(map[string]string{
        "Authorization": "Bearer your-secret-token-here",
    }),
)

provider := sdklog.NewLoggerProvider(
    sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
)
```

#### Python Example - Logs

```python
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
import base64

# Without authentication
exporter = OTLPLogExporter(
    endpoint="http://gravwell-host:8080/v1/logs"
)

# With Basic Authentication
credentials = base64.b64encode(b"username:password").decode("utf-8")
exporter = OTLPLogExporter(
    endpoint="http://gravwell-host:8080/v1/logs",
    headers={"Authorization": f"Basic {credentials}"}
)

# With Token Authentication
exporter = OTLPLogExporter(
    endpoint="http://gravwell-host:8080/v1/logs",
    headers={"Authorization": "Bearer your-secret-token-here"}
)

provider = LoggerProvider()
provider.add_log_record_processor(BatchLogRecordProcessor(exporter))
```

#### Java Example

```java
import io.opentelemetry.exporter.otlp.http.metrics.OtlpHttpMetricExporter;
import io.opentelemetry.sdk.metrics.SdkMeterProvider;
import io.opentelemetry.sdk.metrics.export.PeriodicMetricReader;
import java.util.Base64;
import java.util.HashMap;
import java.util.Map;

// Without authentication
OtlpHttpMetricExporter exporter = OtlpHttpMetricExporter.builder()
    .setEndpoint("http://gravwell-host:8080/v1/metrics")
    .build();

// With Basic Authentication
String credentials = Base64.getEncoder().encodeToString("username:password".getBytes());
Map<String, String> headers = new HashMap<>();
headers.put("Authorization", "Basic " + credentials);

OtlpHttpMetricExporter exporter = OtlpHttpMetricExporter.builder()
    .setEndpoint("http://gravwell-host:8080/v1/metrics")
    .addHeader("Authorization", "Basic " + credentials)
    .build();

// With Token Authentication
OtlpHttpMetricExporter exporter = OtlpHttpMetricExporter.builder()
    .setEndpoint("http://gravwell-host:8080/v1/metrics")
    .addHeader("Authorization", "Bearer your-secret-token-here")
    .build();

SdkMeterProvider meterProvider = SdkMeterProvider.builder()
    .registerMetricReader(PeriodicMetricReader.builder(exporter).build())
    .build();
```

### Data Format

#### Metrics Data Format

Metrics are stored in JSON format with the following structure:

```json
{
  "name": "http.server.request.duration",
  "description": "HTTP server request duration",
  "unit": "ms",
  "type": "gauge",
  "data_points": [
    {
      "time_unix_nano": 1704067200000000000,
      "start_time_unix_nano": 1704067200000000000,
      "value": 123.45,
      "value_type": "double",
      "attributes": {
        "http.method": "GET",
        "http.status_code": 200
      }
    }
  ],
  "resource": {
    "service.name": "my-service",
    "host.name": "my-host"
  },
  "scope": {
    "name": "my-instrumentation",
    "version": "1.0.0"
  }
}
```

#### Logs Data Format

The log body is extracted from the OTLP log record and stored as entry data:
```
This is the log message
```

### Enumerated Values

#### Metrics
Resource attributes from OTLP are automatically attached as enumerated values:
- `service.name`
- `host.name`
- `service.version`
- Any other resource attributes

#### Logs
When `Disable-EVs=false` (default), the following are attached as enumerated values:
- `severity_number` - The severity level as a number
- `severity_text` - The severity level as text (e.g., "INFO", "ERROR")
- `trace_id` - The trace ID for correlation with traces
- `span_id` - The span ID for correlation with spans
- `flags` - Log record flags
- All log attributes
- All resource attributes

These can be extracted and queried using Gravwell's enumerated value extraction.

## Protocol Support

The handler accepts OTLP data via HTTP POST with the following content types:

1. **Protobuf** (recommended for efficiency):
   - `Content-Type: application/x-protobuf`
   - `Content-Type: application/protobuf`

2. **JSON** (for debugging or simpler clients):
   - `Content-Type: application/json`

If no Content-Type is specified, the handler attempts to detect the format automatically.

## Standard OTLP Endpoints

The default URLs follow the OpenTelemetry specification for HTTP exporters, making them compatible with all standard OpenTelemetry clients without configuration changes:
- `/v1/metrics` - Metrics endpoint
- `/v1/logs` - Logs endpoint

## Troubleshooting

### Enable Debug Logging

Set `Debug-Posts=true` in your listener configuration to see detailed information about each request:

```ini
[OpenTelemetry-Metrics-Listener "debug"]
    URL="/v1/metrics"
    Tag-Name="otel-debug"
    Debug-Posts=true

[OpenTelemetry-Logs-Listener "debug-logs"]
    URL="/v1/logs"
    Tag-Name="otel-logs-debug"
    Debug-Posts=true
```

This will log:
- Request method and URL
- Number of entries processed
- Total bytes received
- Processing time in milliseconds

### Check Ingester Logs

Monitor the ingester logs for errors:
```bash
tail -f /opt/gravwell/log/gravwell_http_ingester.log
```

### Test with cURL

Test the metrics endpoint with a simple cURL command:

```bash
# Without authentication
curl -X POST http://localhost:8080/v1/metrics \
  -H "Content-Type: application/json" \
  -d '{
    "resourceMetrics": [{
      "resource": {
        "attributes": [
          {"key": "service.name", "value": {"stringValue": "test"}}
        ]
      },
      "scopeMetrics": [{
        "metrics": [{
          "name": "test.metric",
          "gauge": {
            "dataPoints": [{
              "timeUnixNano": "1704067200000000000",
              "asDouble": 42.0
            }]
          }
        }]
      }]
    }]
  }'

# With Basic Authentication
curl -X POST http://localhost:8080/v1/metrics \
  -H "Content-Type: application/json" \
  -u username:password \
  -d '{...}'

# With Token Authentication
curl -X POST http://localhost:8080/v1/metrics \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-secret-token-here" \
  -d '{...}'
```

Test the logs endpoint with a simple cURL command:

```bash
# Without authentication
curl -X POST http://localhost:8080/v1/logs \
  -H "Content-Type: application/json" \
  -d '{
    "resourceLogs": [{
      "resource": {
        "attributes": [
          {"key": "service.name", "value": {"stringValue": "test"}}
        ]
      },
      "scopeLogs": [{
        "logRecords": [{
          "timeUnixNano": "1704067200000000000",
          "severityNumber": 9,
          "severityText": "INFO",
          "body": {"stringValue": "Test log message"},
          "attributes": [
            {"key": "http.method", "value": {"stringValue": "GET"}}
          ]
        }]
      }]
    }]
  }'

# With Basic Authentication
curl -X POST http://localhost:8080/v1/logs \
  -H "Content-Type: application/json" \
  -u username:password \
  -d '{...}'

# With Token Authentication
curl -X POST http://localhost:8080/v1/logs \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-secret-token-here" \
  -d '{...}'
```

## Dependencies

The handler uses official OpenTelemetry protocol buffers:
- `go.opentelemetry.io/proto/otlp/metrics/v1`
- `go.opentelemetry.io/proto/otlp/logs/v1`
- `go.opentelemetry.io/proto/otlp/collector/metrics/v1`
- `go.opentelemetry.io/proto/otlp/collector/logs/v1`
- `go.opentelemetry.io/proto/otlp/common/v1`
- `go.opentelemetry.io/proto/otlp/resource/v1`

## See Also

- [OpenTelemetry Protocol Specification](https://opentelemetry.io/docs/specs/otlp/)
- [OpenTelemetry Metrics API](https://opentelemetry.io/docs/specs/otel/metrics/api/)
- [OpenTelemetry Logs API](https://opentelemetry.io/docs/specs/otel/logs/)
- [Gravwell HTTP Ingester Documentation](https://docs.gravwell.io/ingesters/ingesters.html#http-ingester)
