# Gravwell Fetcher

A powerful data fetcher ingester for Gravwell that collects data from various security and productivity services including Duo, Thinkst Canary, Okta, and Asana.

## Overview

Gravwell Fetcher is a Go-based ingester that collects data from multiple external services and forwards it to Gravwell for analysis. It supports multiple service integrations and can be configured to fetch different types of data from each service.

## Supported Services

- **Duo Security**
  - Admin API
  - Authentication API
  - Activity API
  - Account API

- **Thinkst Canary**
  - Audit logs
  - Incident data

- **Okta**
  - User data
  - System logs
  - Batch processing support

- **Asana** In-Progress
  - Workspace data
  - Project information

## Prerequisites

- Go 1.x or higher
- Access to Gravwell instance
- API credentials for the services you want to integrate

## Installation

1. Clone the repository
2. Install dependencies:
   ```bash
   go mod download
   ```
3. Build the project:
   ```bash
   go build
   ```

## Configuration

The fetcher uses a configuration file (`gravwell_fetcher.conf`) to manage its settings. Below is a detailed explanation of all configuration options.

### Global Configuration

```ini
[Global]
# Unique identifier for this ingester instance
Ingester-UUID="00000000-0000-0000-0000-0000000000"

# Connection timeout in seconds (0 for no timeout)
Connection-Timeout = 0

# Whether to skip TLS verification
Insecure-Skip-TLS-Verify=false

# Backend connection options (uncomment and configure as needed)
#Cleartext-Backend-Target=127.0.0.1:4023
#Encrypted-Backend-Target=127.1.1.1:4024
#Pipe-Backend-Target=/opt/gravwell/comms/pipe

# Cache configuration
#Ingest-Cache-Path=/opt/gravwell/cache/netflow.cache
#Max-Ingest-Cache=1024  # Cache size in MB (max 1GB)

# Logging configuration
Log-Level=INFO
Log-File=/opt/gravwell/log/gravwell_fetcher.log
```

### Duo Security Configuration

Multiple Duo API endpoints can be configured:

```ini
[DuoConf "duo-admin"]
    StartTime="2025-01-01T00:00:01.000Z"  # Initial fetch time
    Domain=""                             # Duo domain
    Key=""                                # Duo API key
    Secret=""                             # Duo API secret
    DuoAPI="admin"                        # API type: admin, authentication, activity
    Tag-Name="duo-admin"                  # Tag for Gravwell

[DuoConf "duo-auth"]
    StartTime="2025-01-01T00:00:01.000Z"
    Domain=""
    Key=""
    Secret=""
    DuoAPI="authentication"
    Tag-Name="duo-auth"

[DuoConf "duo-activity"]
    StartTime="2025-01-01T00:00:01.000Z"
    Domain=""
    Key=""
    Secret=""
    DuoAPI="activity"
    Tag-Name="duo-activity"

[DuoConf "duo-account"]
    StartTime="2025-01-01T00:00:01.000Z"
    Domain=""
    Key=""
    Secret=""
    DuoAPI="activity"
    Tag-Name="duo-account"
```

### Thinkst Canary Configuration

```ini
[ThinkstConf "thinkst-audit"]
    ThinkstAPI="audit"                    # API type: audit, incident
    Token=""                              # Thinkst API token
    Domain="XXXXXXXX.canary.tools"        # Your Thinkst domain
    StartTime="2025-01-01T00:00:01.000Z"  # Initial fetch time
    Tag-Name="thinkst"                    # Tag for Gravwell

[ThinkstConf "thinkst-incident"]
    ThinkstAPI="incident"
    Token=""
    Domain="XXXXXXXX.canary.tools"
    StartTime="2025-01-01T00:00:01.000Z"
    Tag-Name="thinkst"
```

### Okta Configuration

```ini
[OktaConf "okta1"]
    StartTime="2025-01-01T00:00:01.000Z"  # Initial fetch time
    OktaDomain=""                         # Your Okta domain
    OktaToken=""                          # Okta API token
    UserTag=""                            # User-specific tag
    BatchSize=1000                        # Number of records per batch
    MaxBurstSize=100                      # Maximum burst size
    SeedUsers=true                        # Whether to seed user data
    SeedUserStart="2025-01-01T00:00:01.000Z"  # User data start time
    Tag_Name=okta                         # Tag for Gravwell
```

### Asana Configuration - In Progress

```ini
[AsanaConf "default"]
    RateLimit = 6                         # API rate limit
    StartTime = "2025-01-01T00:00:01.000Z"  # Initial fetch time
    Token = ""                            # Asana API token
    Workspace = ""                        # Asana workspace ID
    Tag-Name = "asana"                    # Tag for Gravwell
```

## Usage

1. Copy the example configuration file:
   ```bash
   cp gravwell_fetcher.conf.example gravwell_fetcher.conf
   ```

2. Edit the configuration file with your specific settings:
   - Add your Gravwell connection details
   - Configure the services you want to use
   - Set appropriate API credentials
   - Adjust timeouts and other parameters as needed

3. Run the fetcher:
   ```bash
   ./gravwell_fetcher
   ```

## Features

- Multiple service integrations
- Configurable data collection intervals
- State tracking to prevent duplicate data collection
- Batch processing support
- Secure credential management
- Flexible tagging system
- Comprehensive logging

## Logging

The fetcher provides detailed logging with configurable log levels. Logs are written to the specified log file and include:
- Connection status
- Data collection events
- Error messages
- Service-specific information

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This software is licensed under the BSD 2-clause license. See the LICENSE file for details.

## Support

For support, please contact Gravwell support or refer to the documentation. 