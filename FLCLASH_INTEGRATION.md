# FlClash integration of mihomo-cn2

The kernel source is `ztyawc/mihomo-cn2`, pinned by `.cn2-import`.
The complete CN2 snapshot is retained with the host APIs required by FlClash.
QyProxy `primary`, `cn2`, `cn2_download`, UU custom SNI TCP, CMCC SOCKS5 0x80/0x82,
and the guard against replacement by an official kernel remain enabled.

`.flclash-api` identifies the imported FlClash API revision. The FlClash
repository maintains this integration on `core-cn2` and pins each build by
commit. Synchronization merges subsequent CN2 changes normally and applies
FlClash API deltas separately. The independent initial CN2 history is recorded
once without replacing the already adapted source tree.

FlClash 0.9.1 also requires the TUN congestion-control option from its core
baseline. The integration carries that field, listener forwarding and the
matching sing-tun/mipstack dependencies so Android can bind the current API.
