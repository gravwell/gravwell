# sFlow Implementation TODO

See https://sflow.org/developers/structures.php

## Legend

- ✅ Implemented
- ❌ Not implemented (standard - should be implemented)
- ⏭️ Skipped (deprecated, vendor-specific or too niche)

## Sample Containers

| Format | Structure             | Status |
| ------ | --------------------- | ------ |
| 1      | FlowSample            | ✅     |
| 2      | CounterSample         | ✅     |
| 3      | FlowSampleExpanded    | ✅     |
| 4      | CounterSampleExpanded | ✅     |

Note: Flow sample containers are implemented, but flow sample **records** (sampled_header, extended_switch, etc.) are not - they decode as `UnknownRecord`.

## Counter Data Structures

### Standard sFlow Structures (Enterprise = 0)

| Format | Structure          | Status          | Reference                             |
| ------ | ------------------ | --------------- | ------------------------------------- |
| 1      | if_counters        | ✅              | sFlow Version 5                       |
| 2      | ethernet_counters  | ✅              | sFlow Version 5                       |
| 3      | tokenring_counters | ✅              | sFlow Version 5                       |
| 4      | vg_counters        | ✅              | sFlow Version 5                       |
| 5      | vlan_counters      | ✅              | sFlow Version 5                       |
| 6      | ieee80211_counters | ✅              | sFlow 802.11 Structures               |
| 7      | lag_port_stats     | ✅              | sFlow LAG Counters Structure          |
| 8      | slow_path_counts   | ⏭️              | Fast path / slow path                 |
| 9      | ib_counters        | ⏭️              | sFlow InfiniBand Structures           |
| 10     | sfp                | ⏭️              | sFlow Optical Interface Structures    |
| 1001   | processor          | ✅              | sFlow Version 5                       |
| 1002   | radio_utilization  | ⏭️              | sFlow 802.11 Structures               |
| 1003   | queue_length       | ❌              | sFlow for queue length monitoring     |
| 1004   | of_port            | ✅              | sFlow OpenFlow Structures             |
| 1005   | port_name          | ✅              | sFlow OpenFlow Structures             |
| 2000   | host_descr         | ✅              | sFlow Host Structures                 |
| 2001   | host_adapters      | ✅              | sFlow Host Structures                 |
| 2002   | host_parent        | ✅              | sFlow Host Structures                 |
| 2003   | host_cpu           | ✅              | sFlow Host Structures                 |
| 2004   | host_memory        | ✅              | sFlow Host Structures                 |
| 2005   | host_disk_io       | ✅              | sFlow Host Structures                 |
| 2006   | host_net_io        | ✅              | sFlow Host Structures                 |
| 2007   | mib2_ip_group      | ✅              | sFlow Host TCP/IP Counters            |
| 2008   | mib2_icmp_group    | ✅              | sFlow Host TCP/IP Counters            |
| 2009   | mib2_tcp_group     | ✅              | sFlow Host TCP/IP Counters            |
| 2010   | mib2_udp_group     | ✅              | sFlow Host TCP/IP Counters            |
| 2100   | virt_node          | ✅              | sFlow Host Structures                 |
| 2101   | virt_cpu           | ✅              | sFlow Host Structures                 |
| 2102   | virt_memory        | ✅              | sFlow Host Structures                 |
| 2103   | virt_disk_io       | ✅              | sFlow Host Structures                 |
| 2104   | virt_net_io        | ✅              | sFlow Host Structures                 |
| 2105   | jmx_runtime        | ✅              | sFlow Java Virtual Machine Structures |
| 2106   | jmx_statistics     | ✅              | sFlow Java Virtual Machine Structures |
| 2200   | memcached_counters | ⏭️ (deprecated) | sFlow for memcached                   |
| 2201   | http_counters      | ✅              | sFlow HTTP Structures                 |
| 2202   | app_operations     | ✅              | sFlow Application Structures          |
| 2203   | app_resources      | ✅              | sFlow Application Structures          |
| 2204   | memcache_counters  | ✅              | sFlow Memcache Structures             |
| 2206   | app_workers        | ✅              | sFlow Application Structures          |
| 2207   | ovs_dp_stats       | ❌              | Open vSwitch performance monitoring   |
| 3000   | energy             | ✅              | Energy management                     |
| 3001   | temperature        | ✅              | Energy management                     |
| 3002   | humidity           | ✅              | Energy management                     |
| 3003   | fans               | ✅              | Energy management                     |

### Vendor-Specific Structures (Enterprise ≠ 0) - Skipped

| Enterprise | Format | Structure          | Reason          | Reference                                    |
| ---------- | ------ | ------------------ | --------------- | -------------------------------------------- |
| 44131      | -      | bst_device_buffers | Vendor-specific | sFlow Broadcom Peak Buffer Utilization       |
| 44132      | -      | bst_port_buffers   | Vendor-specific | sFlow Broadcom Peak Buffer Utilization       |
| 44133      | -      | hw_tables          | Vendor-specific | sFlow Broadcom Switch ASIC Table Utilization |
| 57031      | -      | nvidia_gpu         | Vendor-specific | sFlow NVML GPU Structures                    |

## Flow Data Structures

### Standard sFlow Structures (Enterprise = 0)

| Format | Structure                         | Status          | Reference                         |
| ------ | --------------------------------- | --------------- | --------------------------------- |
| 1      | sampled_header                    | ❌              | sFlow Version 5                   |
| 2      | sampled_ethernet                  | ❌              | sFlow Version 5                   |
| 3      | sampled_ipv4                      | ❌              | sFlow Version 5                   |
| 4      | sampled_ipv6                      | ❌              | sFlow Version 5                   |
| 1001   | extended_switch                   | ❌              | sFlow Version 5                   |
| 1002   | extended_router                   | ❌              | sFlow Version 5                   |
| 1003   | extended_gateway                  | ❌              | sFlow Version 5                   |
| 1004   | extended_user                     | ❌              | sFlow Version 5                   |
| 1005   | extended_url                      | ⏭️ (deprecated) | sFlow Version 5                   |
| 1006   | extended_mpls                     | ⏭️              | sFlow Version 5                   |
| 1007   | extended_nat                      | ❌              | sFlow Version 5                   |
| 1008   | extended_mpls_tunnel              | ⏭️              | sFlow Version 5                   |
| 1009   | extended_mpls_vc                  | ⏭️              | sFlow Version 5                   |
| 1010   | extended_mpls_FTN                 | ⏭️              | sFlow Version 5                   |
| 1011   | extended_mpls_LDP_FEC             | ⏭️              | sFlow Version 5                   |
| 1012   | extended_vlantunnel               | ⏭️              | sFlow Tunnel Structures           |
| 1013   | extended_80211_payload            | ⏭️              | sFlow 802.11 Structures           |
| 1014   | extended_80211_rx                 | ⏭️              | sFlow 802.11 Structures           |
| 1015   | extended_80211_tx                 | ⏭️              | sFlow 802.11 Structures           |
| 1016   | extended_80211_aggregation        | ⏭️              | sFlow 802.11 Structures           |
| 1017   | extended_openflow_v1              | ⏭️ (deprecated) | sFlow OpenFlow Structures         |
| 1018   | extended_fcs                      | ❌              | sFlow, CEE and FCoE               |
| 1019   | extended_queue_length             | ❌              | sFlow for queue length monitoring |
| 1020   | extended_nat_ports                | ❌              | sFlow Port NAT Structures         |
| 1021   | extended_L2_tunnel_egress         | ⏭️              | sFlow Tunnel Structures           |
| 1022   | extended_L2_tunnel_ingress        | ⏭️              | sFlow Tunnel Structures           |
| 1023   | extended_ipv4_tunnel_egress       | ⏭️              | sFlow Tunnel Structures           |
| 1024   | extended_ipv4_tunnel_ingress      | ⏭️              | sFlow Tunnel Structures           |
| 1025   | extended_ipv6_tunnel_egress       | ⏭️              | sFlow Tunnel Structures           |
| 1026   | extended_ipv6_tunnel_ingress      | ⏭️              | sFlow Tunnel Structures           |
| 1027   | extended_decapsulate_egress       | ⏭️              | sFlow Tunnel Structures           |
| 1028   | extended_decapsulate_ingress      | ⏭️              | sFlow Tunnel Structures           |
| 1029   | extended_vni_egress               | ⏭️              | sFlow Tunnel Structures           |
| 1030   | extended_vni_ingress              | ⏭️              | sFlow Tunnel Structures           |
| 1031   | extended_ib_lrh                   | ⏭️              | sFlow InfiniBand Structures       |
| 1032   | extended_ib_grh                   | ⏭️              | sFlow InfiniBand Structures       |
| 1033   | extended_ib_brh                   | ⏭️              | sFlow InfiniBand Structures       |
| 1034   | extended_vlanin                   | ❌              | sFlow QinQ related statistics     |
| 1035   | extended_vlanout                  | ❌              | sFlow QinQ related statistics     |
| 1036   | extended_egress_queue             | ⏭️              | sFlow Dropped Packet Notification |
| 1037   | extended_acl                      | ⏭️              | sFlow Dropped Packet Notification |
| 1038   | extended_function                 | ⏭️              | sFlow Dropped Packet Notification |
| 1039   | extended_transit                  | ❌              | sFlow Transit Delay Structures    |
| 1040   | extended_queue                    | ❌              | sFlow Transit Delay Structures    |
| 1041   | extended_hw_trap                  | ❌              | sFlow Host                        |
| 1042   | extended_linux_drop_reason        | ❌              | sFlow Host                        |
| 1043   | extended_timestamp                | ❌              | sFlow Host                        |
| 2000   | transaction                       | ⏭️              | Host performance statistics       |
| 2001   | extended_nfs_storage_transaction  | ⏭️              | Host performance statistics       |
| 2002   | extended_scsi_storage_transaction | ⏭️              | Host performance statistics       |
| 2003   | extended_http_transaction         | ⏭️              | Host performance statistics       |
| 2100   | extended_socket_ipv4              | ❌              | sFlow Host Structures             |
| 2101   | extended_socket_ipv6              | ❌              | sFlow Host Structures             |
| 2102   | extended_proxy_socket_ipv4        | ❌              | sFlow HTTP Structures             |
| 2103   | extended_proxy_socket_ipv6        | ❌              | sFlow HTTP Structures             |
| 2200   | memcached_operations              | ⏭️              | sFlow Memcache Structures         |
| 2201   | http_request                      | ⏭️ (deprecated) | sFlow for HTTP                    |
| 2202   | app_operations                    | ⏭️              | sFlow Application Structures      |
| 2203   | app_parent_context                | ⏭️              | sFlow Application Structures      |
| 2204   | app_initiators                    | ⏭️              | sFlow Application Structures      |
| 2205   | app_targets                       | ⏭️              | sFlow Application Structures      |
| 2206   | http_requests                     | ⏭️              | sFlow HTTP Structures             |
| 2207   | extended_proxy_request            | ⏭️              | sFlow HTTP Structures             |
| 2208   | extended_nav_timing               | ⏭️              | Navigation Timing                 |
| 2209   | extended_tcp_info                 | ❌              | TCP performance                   |
| 2210   | extended_entities                 | ❌              | Systemd traffic marking           |

### Vendor-Specific Flow Data Structures (Enterprise ≠ 0) - Skipped

| Enterprise | Format | Structure        | Reason          | Reference                              |
| ---------- | ------ | ---------------- | --------------- | -------------------------------------- |
| 43         | 1002   | rtmetric         | Vendor-specific | Custom Metrics                         |
| 43         | 1003   | rtflow           | Vendor-specific | Custom Metrics                         |
| 44131      | -      | bst_egress_queue | Vendor-specific | sFlow Broadcom Peak Buffer Utilization |
