# sFlow

Go library for decoding sFlow v5 datagrams (RFC 3176). Implements counter and flow structures prioritized for network monitoring and log ingestion use cases.

## Implementation Status

All sample containers (FlowSample, CounterSample, FlowSampleExpanded, CounterSampleExpanded, DiscardedPacket) are implemented.

**Legend:** ✅ = implemented, ✅✅ = implemented with test coverage, ❌ = not implemented

### Counter Records (Enterprise = 0)

| Format | Structure          | Status |
| ------ | ------------------ | ------ |
| 1      | if_counters        | ✅     |
| 2      | ethernet_counters  | ✅     |
| 3      | tokenring_counters | ✅     |
| 4      | vg_counters        | ✅     |
| 5      | vlan_counters      | ✅     |
| 6      | ieee80211_counters | ✅     |
| 7      | lag_port_stats     | ✅     |
| 8      | slow_path_counts   | ❌     |
| 9      | ib_counters        | ❌     |
| 10     | sfp                | ❌     |
| 1001   | processor          | ✅     |
| 1002   | radio_utilization  | ❌     |
| 1003   | queue_length       | ✅     |
| 1004   | of_port            | ✅     |
| 1005   | port_name          | ✅✅   |
| 2000   | host_descr         | ✅✅   |
| 2001   | host_adapters      | ✅✅   |
| 2002   | host_parent        | ✅✅   |
| 2003   | host_cpu           | ✅✅   |
| 2004   | host_memory        | ✅✅   |
| 2005   | host_disk_io       | ✅✅   |
| 2006   | host_net_io        | ✅✅   |
| 2007   | mib2_ip_group      | ✅✅   |
| 2008   | mib2_icmp_group    | ✅✅   |
| 2009   | mib2_tcp_group     | ✅✅   |
| 2010   | mib2_udp_group     | ✅✅   |
| 2100   | virt_node          | ✅✅   |
| 2101   | virt_cpu           | ✅✅   |
| 2102   | virt_memory        | ✅✅   |
| 2103   | virt_disk_io       | ✅✅   |
| 2104   | virt_net_io        | ✅✅   |
| 2105   | jmx_runtime        | ✅     |
| 2106   | jmx_statistics     | ✅     |
| 2200   | memcached_counters | ❌     |
| 2201   | http_counters      | ✅     |
| 2202   | app_operations     | ✅     |
| 2203   | app_resources      | ✅     |
| 2204   | memcache_counters  | ✅     |
| 2206   | app_workers        | ✅     |
| 2207   | ovs_dp_stats       | ✅     |
| 3000   | energy             | ✅     |
| 3001   | temperature        | ✅     |
| 3002   | humidity           | ✅     |
| 3003   | fans               | ✅     |

### Flow Records (Enterprise = 0)

| Format | Structure                      | Status |
| ------ | ------------------------------ | ------ |
| 1      | sampled_header                 | ✅✅   |
| 2      | sampled_ethernet               | ✅✅   |
| 3      | sampled_ipv4                   | ✅     |
| 4      | sampled_ipv6                   | ✅     |
| 1001   | extended_switch                | ✅✅   |
| 1002   | extended_router                | ✅     |
| 1003   | extended_gateway               | ✅     |
| 1004   | extended_user                  | ✅     |
| 1005   | extended_url                   | ❌     |
| 1006   | extended_mpls                  | ✅     |
| 1007   | extended_nat                   | ✅     |
| 1008   | extended_mpls_tunnel           | ✅     |
| 1009   | extended_mpls_vc               | ✅     |
| 1010   | extended_mpls_FTN              | ✅     |
| 1011   | extended_mpls_LDP_FEC          | ✅     |
| 1012   | extended_vlantunnel            | ✅     |
| 1013   | extended_80211_payload         | ❌     |
| 1014   | extended_80211_rx              | ❌     |
| 1015   | extended_80211_tx              | ❌     |
| 1016   | extended_80211_aggregation     | ❌     |
| 1017   | extended_openflow_v1           | ❌     |
| 1018   | extended_fcs                   | ❌     |
| 1019   | extended_queue_length          | ❌     |
| 1020   | extended_nat_ports             | ❌     |
| 1021   | extended_L2_tunnel_egress      | ❌     |
| 1022   | extended_L2_tunnel_ingress     | ❌     |
| 1023   | extended_ipv4_tunnel_egress    | ❌     |
| 1024   | extended_ipv4_tunnel_ingress   | ❌     |
| 1025   | extended_ipv6_tunnel_egress    | ❌     |
| 1026   | extended_ipv6_tunnel_ingress   | ❌     |
| 1027   | extended_decapsulate_egress    | ❌     |
| 1028   | extended_decapsulate_ingress   | ❌     |
| 1029   | extended_vni_egress            | ❌     |
| 1030   | extended_vni_ingress           | ❌     |
| 1031   | extended_ib_lrh                | ❌     |
| 1032   | extended_ib_grh                | ❌     |
| 1033   | extended_ib_brh                | ❌     |
| 1034   | extended_vlanin                | ❌     |
| 1035   | extended_vlanout               | ❌     |
| 1036   | extended_egress_queue          | ✅✅   |
| 1037   | extended_acl                   | ✅✅   |
| 1038   | extended_function              | ✅✅   |
| 1039   | extended_transit               | ❌     |
| 1040   | extended_queue                 | ❌     |
| 1041   | extended_hw_trap               | ❌     |
| 1042   | extended_linux_drop_reason     | ❌     |
| 1043   | extended_timestamp             | ❌     |
| 2000   | transaction                    | ❌     |
| 2001   | extended_nfs_storage_txn       | ❌     |
| 2002   | extended_scsi_storage_txn      | ❌     |
| 2003   | extended_http_transaction      | ❌     |
| 2100   | extended_socket_ipv4           | ✅     |
| 2101   | extended_socket_ipv6           | ✅     |
| 2102   | extended_proxy_socket_ipv4     | ❌     |
| 2103   | extended_proxy_socket_ipv6     | ❌     |
| 2200   | memcached_operations           | ❌     |
| 2201   | http_request                   | ❌     |
| 2202   | app_operations                 | ❌     |
| 2203   | app_parent_context             | ❌     |
| 2204   | app_initiators                 | ❌     |
| 2205   | app_targets                    | ❌     |
| 2206   | http_requests                  | ❌     |
| 2207   | extended_proxy_request         | ❌     |
| 2208   | extended_nav_timing            | ❌     |
| 2209   | extended_tcp_info              | ✅✅   |
| 2210   | extended_entities              | ❌     |

Unknown sample formats decode as `UnknownSample` with raw data preserved. Unknown flow and counter record formats decode as `UnknownRecord` with raw data preserved. This allows the decoder to process datagrams containing unimplemented or vendor-specific structures without error.

**Want a structure implemented or tested?** Send us a raw sFlow packet capture containing it.

## Architecture

- `sflow.go`: Top-level API
- `datagram/`: Low-level XDR types matching sFlow wire format
- `decoder/`: Higher-level decoded types with proper Go field names
- `xdr/`: RFC 4506 XDR encoding/decoding primitives
- `internal/cmd/testgen/`: Utility for capturing live sFlow packets and generating test cases

## Security

The decoder protects against OOM attacks by limiting datagrams to 65,536 bytes via `io.LimitedReader` and validating all count fields against remaining bytes before allocation. Uses `uint64` arithmetic to prevent integer overflow bypasses.

## Testing

The `internal/cmd/testgen` binary captures live sFlow packets as `.bin` files. Run `sflowtool-ref.sh` on the captured files to generate `.json` reference output from the official sflowtool (via Docker) for validation.

## References

- https://sflow.org/developers/structures.php
- https://datatracker.ietf.org/doc/html/rfc3176 (sFlow v5)
- https://datatracker.ietf.org/doc/html/rfc4506 (XDR)
