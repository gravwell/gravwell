# Gravwell Fetcher

A powerful data fetcher ingester for Gravwell that collects data from various security and productivity services including Duo, Thinkst Canary, Okta, and Asana.

## Overview

Gravwell Fetcher is a Go-based ingester that collects data from multiple external services and forwards it to Gravwell for analysis. It supports multiple service integrations and can be configured to fetch different types of data from each service.

## Supported Services

- **Asana** In-Progress
	- Workspace data Logs
	- Project information Logs

- **CrowdStrike** In-Progress
	- Event Stream Logs
	- Alerts/Detections Logs
	- Incidents Logs
	- (RTR) Audit Logs
	- Hosts/Devices Logs

- **Duo Security**
	- Account Logs
	- Activity Logs
	- Admin Logs
	- Authentication Logs

- **Mimecast**
	- Audit Logs
	- MTA Delivery Logs
	- MTA Receipt Logs
	- MTA Process Logs
	- MTA AV Logs
	- MTA Spam Logs
	- MTA Internal Logs
	- MTA Impersonation Logs
	- MTA URL Logs
	- MTA Attachment Logs
	- MTA Journal Logs

- **Okta**
	- Groups Logs
	- System Logs
	- Users Logs

- **Mimecast**
  - Audit
  - MTA
    - Attachment
    - AV
    - Delivery
    - Impersonation
    - Internal
    - Journal
    - Process
    - Reciept
    - Spam
    - URL

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

### Asana Configuration - In Progress

```ini
[AsanaConf "default"]
	RateLimit = 6                         # API rate limit
	StartTime = "2025-01-01T00:00:01.000Z"  # Initial fetch time
	Token = ""                            # Asana API token
	Workspace = ""                        # Asana workspace ID
	Tag-Name = "asana"                    # Tag for Gravwell
```

### CrowdStrike Configuration - In Progress

Multiple CrowdStrike API endpoints can be configured:

```
# CrowdStrike
# Make sure to set your tenant region properly within Domain:
	# US-1: https://api.crowdstrike.com
	# US-2: https://api.us-2.crowdstrike.com
	# EU-1: https://api.eu-1.crowdstrike.com
	# GCW : https://api.laggar.gcw.crowdstrike.com

# CrowdStrike: Event Stream (datafeed v2) — requires a stable AppID
[CrowdStrikeConf "crowdstrike-stream"]
	StartTime="2025-10-01T00:00:01.000Z"
	Domain="https://api.crowdstrike.com"
	Key="REPLACE_WITH_YOUR_CROWDSTRIKE_KEY"
	Secret="REPLACE_WITH_YOUR_CROWDSTRIKE_SECRET"
	AppID="gravwellfetcher01"	 # alphanumeric ≤20; unique per tenant recommended
	APIType="stream"
	Tag-Name="crowdstrike-stream"
	RateLimit=60

# CrowdStrike: Alerts
[CrowdStrikeConf "crowdstrike-alerts"]
	StartTime="2025-10-01T00:00:01.000Z"
	Domain="https://api.crowdstrike.com"
	Key="REPLACE_WITH_YOUR_CROWDSTRIKE_KEY"
	Secret="REPLACE_WITH_YOUR_CROWDSTRIKE_SECRET"
	APIType="detections"		  # will route to Alerts v2 if detections fails
	Tag-Name="crowdstrike-alerts"
	RateLimit=60

# CrowdStrike: Incidents (SDK, paginated)
[CrowdStrikeConf "crowdstrike-incidents"]
	StartTime="2025-10-01T00:00:01.000Z"
	Domain="https://api.crowdstrike.com"
	Key="REPLACE_WITH_YOUR_CROWDSTRIKE_KEY"
	Secret="REPLACE_WITH_YOUR_CROWDSTRIKE_SECRET"
	APIType="incidents"
	Tag-Name="crowdstrike-incidents"
	RateLimit=60

# CrowdStrike: (RTR) Audit
[CrowdStrikeConf "crowdstrike-rtr-audit"]
	StartTime="2025-10-01T00:00:01.000Z"
	Domain="https://api.crowdstrike.com"
	Key="REPLACE_WITH_YOUR_CROWDSTRIKE_KEY"
	Secret="REPLACE_WITH_YOUR_CROWDSTRIKE_SECRET"
	APIType="audit"		# will route to RTR Audit if audit fails
	Tag-Name="crowdstrike-audit"
	RateLimit=60

# CrowdStrike: Hosts / Devices
[CrowdStrikeConf "crowdstrike-hosts"]
	StartTime="2025-10-01T00:00:01.000Z"
	Domain="https://api.crowdstrike.com"
	Key="REPLACE_WITH_YOUR_CROWDSTRIKE_KEY"
	Secret="REPLACE_WITH_YOUR_CROWDSTRIKE_SECRET"
	APIType="hosts"
	Tag-Name="crowdstrike-hosts"
	RateLimit=60
```

### Duo Security Configuration

Multiple Duo API endpoints can be configured:

```ini
# Duo: Account
[DuoConf "duo-account"]
	StartTime="2025-01-01T00:00:01.000Z"
	Domain="api-XXXXXXXX.duosecurity.com"
	Key="REPLACE_WITH_YOUR_DUO_KEY"
	Secret="REPLACE_WITH_YOUR_DUO_SECRET"
	DuoAPI="account"
	Tag-Name="duo-account"

# Duo: Activity
[DuoConf "duo-activity"]
	StartTime="2025-01-01T00:00:01.000Z"
	Domain="api-XXXXXXXX.duosecurity.com"
	Key="REPLACE_WITH_YOUR_DUO_KEY"
	Secret="REPLACE_WITH_YOUR_DUO_SECRET"
	DuoAPI="activity"
	Tag-Name="duo-activity"

# Duo: Admin
[DuoConf "duo-admin"]
	StartTime="2025-01-01T00:00:01.000Z"
	Domain="api-XXXXXXXX.duosecurity.com"
	Key="REPLACE_WITH_YOUR_DUO_KEY"
	Secret="REPLACE_WITH_YOUR_DUO_SECRET"
	DuoAPI="admin"
	Tag-Name="duo-admin"

# Duo: Authentication
[DuoConf "duo-auth"]
	StartTime="2025-01-01T00:00:01.000Z"
	Domain="api-XXXXXXXX.duosecurity.com"
	Key="REPLACE_WITH_YOUR_DUO_KEY"
	Secret="REPLACE_WITH_YOUR_DUO_SECRET"
	DuoAPI="authentication"
	Tag-Name="duo-auth"
```

### Mimecast

Multiple Mimecast API endpoints can be configured:

```
# Mimecast: Audit
[MimecastConf "mimecast-audit"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="w0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="audit"
	StartTime="2025-06-20T00:00:01.000Z"
	Tag-Name="mimecast-audit"

# Mimecast: MTA Delivery
[MimecastConf "mimecast-mta-delivery"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-delivery"
	Tag-Name="mimecast-delivery"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA Receipt 
[MimecastConf "mimecast-mta-receipt"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-receipt"
	Tag-Name="mimecast-receipt"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA Process
[MimecastConf "mimecast-mta-process"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-process"
	Tag-Name="mimecast-process"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA AV
[MimecastConf "mimecast-mta-av"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-av"
	Tag-Name="mimecast-av"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA Spam
[MimecastConf "mimecast-mta-spam"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-spam"
	Tag-Name="mimecast-spam"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA Internal
[MimecastConf "mimecast-mta-internal"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-internal"
	Tag-Name="mimecast-internal"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA Impersonation
[MimecastConf "mimecast-mta-impersonation"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-impersonation"
	Tag-Name="mimecast-impersonation"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA URL
[MimecastConf "mimecast-mta-url"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-url"
	Tag-Name="mimecast-url"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA Attachment
[MimecastConf "mimecast-mta-attachment"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-attachment"
	Tag-Name="mimecast-attachment"
	StartTime="2025-07-29T00:00:01.000Z"

# Mimecast: MTA Journal
[MimecastConf "mimecast-mta-journal"]
	ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	MimecastAPI="mta-journal"
	Tag-Name="mimecast-journal"
	StartTime="2025-07-29T00:00:01.000Z"
```

### Okta Configuration

Multiple Okta API endpoints can be configured:

```ini
# Okta: Groups
[OktaConf "okta-groups"]
	StartTime="2025-01-01T00:00:01.000Z"
	OktaDomain="https://REPLACE_WITH_YOUR_OKTA_ORG.okta.com"
	OktaToken="REPLACE_WITH_YOUR_OKTA_TOKEN"
	BatchSize=1000
	MaxBurstSize=100
	SeedUsers=false
	RateLimit=30
	Preprocessor=json
	Tag-Name="okta-groups"

# Okta: System	
[OktaConf "okta-system"]
	StartTime="2025-01-01T00:00:01.000Z"
	OktaDomain="https://REPLACE_WITH_YOUR_OKTA_ORG.okta.com"
	OktaToken="REPLACE_WITH_YOUR_OKTA_TOKEN"
	BatchSize=1000
	MaxBurstSize=100
	SeedUsers=false
	SeedUserStart="2025-01-01T00:00:01.000Z"
	Tag-Name="okta-system"
	RateLimit=60
	Preprocessor=json

# Okta: Users
[OktaConf "okta-users"]
	StartTime="2025-01-01T00:00:01.000Z"
	OktaDomain="https://REPLACE_WITH_YOUR_OKTA_ORG.okta.com"
	OktaToken="REPLACE_WITH_YOUR_OKTA_TOKEN"
	BatchSize=1000
	MaxBurstSize=100
	SeedUsers=true
	SeedUserStart="2025-01-01T00:00:01.000Z"
	Tag-Name="okta-users"
	RateLimit=60
	Preprocessor=json
```

### Mimecast Configuration
> **Note:**
> The `StartTime` cannot be more than 7 days in the past
```ini
[MimecastConf "mimecast-audit"]
        ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
        ClientSecret="w0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
        MimecastAPI="audit"
        StartTime="2025-06-20T00:00:01.000Z"
        Tag-Name="mimecast-audit"

[MimecastConf "mimecast-mta-delivery"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-delivery"
       Tag-Name="mimecast-delivery"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-reciept"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-receipt"
       Tag-Name="mimecast-receipt"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-process"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-process"
       Tag-Name="mimecast-process"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-av"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-av"
       Tag-Name="mimecast-av"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-spam"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-spam"
       Tag-Name="mimecast-spam"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-internal"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-internal"
       Tag-Name="mimecast-internal"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-impersonation"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-impersonation"
       Tag-Name="mimecast-impersonation"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-url"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-url"
       Tag-Name="mimecast-url"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-attachment"]
       ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
       MimecastAPI="mta-attachment"
       Tag-Name="mimecast-attachment"
       StartTime="2025-07-29T00:00:01.000Z"

[MimecastConf "mimecast-mta-journal"]
        ClientID="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
        ClientSecret="1Mpgw0wfXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
        MimecastAPI="mta-journal"
        Tag-Name="mimecast-journal"
        StartTime="2025-07-29T00:00:01.000Z"
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

	- If you individualize vendors, run it with its specific configuration (e.g. for Duo):
	```bash
	./duo_fetcher -config-file /opt/gravwell_fetcher/etc/duo_fetcher.conf
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