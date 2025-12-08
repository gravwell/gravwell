# sFlow Implementation Priorities

Prioritization guide for the sFlow decoder. See `TODO.md` for implementation status.

## Critical Gap

**Flow Samples (formats 1, 3) are NOT implemented.** They decode as `UnknownSample`, discarding all packet/traffic data. For log ingestion, Flow Samples are the primary data source.

---

## Will Implement

### Priority 1: Critical (Flow Samples)

Without these, the decoder cannot process flow data - the primary sFlow use case.

| Format | Structure              | Why                                            |
| ------ | ---------------------- | ---------------------------------------------- |
| -      | FlowSample (format 1)  | Sample container - required for any flow data  |
| -      | FlowSampleExpanded (3) | Expanded container for large ifIndex values    |
| 1      | sampled_header         | Raw packet headers - most valuable flow record |
| 2      | sampled_ethernet       | Ethernet frame info (fallback)                 |
| 3      | sampled_ipv4           | IPv4 packet info (fallback)                    |
| 4      | sampled_ipv6           | IPv6 packet info (fallback)                    |

### Priority 2: High (Common Extended Flow Data)

Commonly emitted by switches/routers alongside flow samples.

| Format | Structure        | Why                                       |
| ------ | ---------------- | ----------------------------------------- |
| 1001   | extended_switch  | VLAN tagging - very common                |
| 1002   | extended_router  | Next-hop, prefix masks - common in routed |
| 1003   | extended_gateway | BGP info - valuable for ISP/peering       |
| 1007   | extended_nat     | NAT translation - common in enterprise    |

### Priority 3: Medium (Host/App Flow Data)

| Format | Structure            | Why                                |
| ------ | -------------------- | ---------------------------------- |
| 2100   | extended_socket_ipv4 | Links app flows to network         |
| 2101   | extended_socket_ipv6 | IPv6 socket info                   |
| 1004   | extended_user        | User identity - auth correlation   |
| 2209   | extended_tcp_info    | TCP performance (RTT, retransmits) |

### Priority 4: Low (Counter Siblings)

Completes the Host sFlow family - these appear alongside already-implemented `host_*` counters from `hsflowd`.

| Format | Structure       | Why                                  |
| ------ | --------------- | ------------------------------------ |
| 2007   | mib2_ip_group   | IP stats - forwarding, errors, frag  |
| 2008   | mib2_icmp_group | ICMP stats - connectivity issues     |
| 2009   | mib2_tcp_group  | TCP stats - connections, retransmits |
| 2010   | mib2_udp_group  | UDP stats - datagrams, errors        |
| 2207   | ovs_dp_stats    | Open vSwitch - pairs with virt\_\*   |
| 1003   | queue_length    | Congestion analysis                  |

---

## Won't Implement

### Deprecated

| Format | Structure            |
| ------ | -------------------- |
| 1005   | extended_url         |
| 1017   | extended_openflow_v1 |
| 2201   | http_request         |

### Too Niche

Not worth the implementation effort for log ingestion use cases.

| Formats         | Group                  | Why Skip                        |
| --------------- | ---------------------- | ------------------------------- |
| 1006, 1008-1011 | MPLS structures        | MPLS-specific deployments only  |
| 1012            | extended_vlantunnel    | Q-in-Q is rare                  |
| 1013-1016       | 802.11 flow structures | Wireless sFlow is uncommon      |
| 1021-1030       | Tunnel structures      | VxLAN/NVGRE - can add if needed |
| 1031-1033       | InfiniBand structures  | HPC-specific                    |
| 1036-1038       | Dropped packet         | Can add if needed               |
| 2000-2003       | Transaction structures | Rarely used                     |
| 2200            | memcached_operations   | Memcache sFlow is rare          |
| 2202-2205       | Application structures | App sFlow has limited adoption  |
| 2206-2208       | HTTP/proxy structures  | HTTP sFlow is rare              |

### Low-Value Counters

| Format | Structure         | Why Skip                  |
| ------ | ----------------- | ------------------------- |
| 8      | slow_path_counts  | Very specialized          |
| 9      | ib_counters       | InfiniBand only           |
| 10     | sfp               | Optical interface metrics |
| 1002   | radio_utilization | Wireless - niche          |
