# QyProxy 游戏加速出站

新增 `type: qyproxy`，支持 `primary`（普通游戏主线）、`cn2`、
`cn2_download` 三个角色。由 Go 内核直接连接节点，无需 Python 中转、
原 APP 进程或其 native 库。

这里的主线是已验证的 QyProxy / APP `mode=8` 主线；QyVpn / `mode=2`
及 Hop 多跳属于其他协议，不能使用此出站。

## 配置

以下地址与凭据都是占位内容，使用时替换为自己当前有效的节点配置。

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
ipv6: false

proxies:
  - &qy
    name: 游戏主线
    type: qyproxy
    server: 198.51.100.10
    port: 7025
    role: primary
    username: "YOUR_NODE_USERNAME"
    password: "YOUR_NODE_PASSWORD"
    sn: "YOUR_DEVICE_SN"
    game-id: 19535
    server-id: 21254
    game-area: hk
    zone: hk
    client-type: 20
    product: hy-android
    version: "1.0.0"

  - <<: *qy
    name: CN2
    server: 198.51.100.11
    role: cn2
    username: "YOUR_CN2_USERNAME"
    password: "YOUR_CN2_SECRET"

  - <<: *qy
    name: CN2 下载
    server: 198.51.100.12
    role: cn2_download
    username: "YOUR_CN2_USERNAME"
    password: "YOUR_CN2_SECRET"

proxy-groups:
  - name: 游戏加速
    type: select
    proxies: [CN2, CN2 下载, 游戏主线]

rules:
  - MATCH,游戏加速
```

字段说明：

| 字段 | 含义 / 默认值 |
|---|---|
| `server`、`port` | 必填；实际节点与控制 TCP 端口。本次样本使用 7025，不能直接复制原 profile 的通用 9999 |
| `role` | `primary`（默认）、`cn2`、`cn2_download` |
| `username` | 未拼接的节点用户名，内核会追加客户端类型、游戏 ID 和 SN |
| `password` | 必填；主线节点密码，或 CN2 角色的专用 secret，两者来源不同 |
| `sn` | 必填；原配置的设备标识 |
| `game-id` | 必填；原配置 `gameinfo.game_id` |
| `server-id` | 必填；原配置 `client.serverid`，本次样本对应区服 ID |
| `game-area` | 原配置 `customProfile.qyproxy_config.game_area`；默认 `default` |
| `zone` | 原配置 `client.select_zone_flag`；默认等于 `game-area` |
| `client-type` | 默认 20 |
| `product`、`version` | 原 `product_line.type/version`；默认 `hy-android` / `1.0.0` |

三个角色都支持 TCP/UDP，无需单独启用 `udp`。支持公共出站选项
`interface-name`、`routing-mark`、`dialer-proxy`、`tfo`、`mptcp`；
上游和目标使用 IPv4。链式出站承载 UDP 时，上一级也需要支持 UDP。

## 会话与 DNS

- 每个出站首次收到请求时独立认证；并发请求共享一次登录和一条控制连接。
- 使用 CHAP-MD5，解析 `CODE=3/5/7` 成功格式。CODE=5/7 有实际节点验证，
  CODE=3 由静态分析和模拟节点测试覆盖。
- 数据端口、token 从本次握手取得；三种角色的 token 不混用。
- 断线或心跳超时会关闭旧数据连接、丢弃 DNS 缓存，再完整认证；重试间隔
  为 2、5、10 秒。认证拒绝或重试耗尽后，需要更新配置或重新加载出站。
- TCP 的双向 XOR 偏移独立；UDP 每包重新开始。TCP 支持半关闭和超时。
- 主线使用节点协商的 DNS 地址、端口及区域前缀；CN2 通过自己的数据
  隧道向 `1.1.1.1:53` 发送普通 DNS。CN2 ACK 不要求提供专用 DNS 端口。
- 域名在相应隧道内解析，缓存跟随认证会话；mihomo 的显式 hosts/IP
  映射会保留。fake-IP 携带的原始域名会被解析，不将假地址发送到节点。
- 转发 UDP DNS 时还原客户端原始解析器地址与事务 ID；不会在失败后自动直连。
- 日志不包含密码、SN、token、Challenge 或完整握手内容。

## 当前边界

已实现 IPv4 TCP、未分片 UDP、CHAP 和明文 TCP 控制连接。最大出站 UDP
帧为主线 1400 字节、CN2 1200 字节，包含 10 字节协议头及可能的 DNS
前缀；超过限制返回错误。入站的协议分片暂不支持。

尚未实现 QyVpn、Hop、PAP/token 登录、控制连接 CODE=9 恢复、节点 TLS、
IPv6、UDP 协议分片及账号/节点配置自动获取和续期。重新认证不会无损
续接旧 TCP 连接。线路名字和游戏区服不保证公网出口地区或任意站点可用。

手机直接使用需要客户端加载此定制 mihomo 内核。仅导入配置到不认识
`qyproxy` 的官方内核或其他代理内核不会生效。

## 开发验证

```sh
go test ./transport/qyproxy ./adapter ./adapter/outbound ./transport/socks5
CGO_ENABLED=1 go test -race ./transport/qyproxy ./adapter ./adapter/outbound
SKIP_CONCURRENT_TEST=1 SKIP_INTEROP_TEST=1 go test ./...
go build -tags with_gvisor .
```

测试使用合成账号和本地模拟服务器。编码基准来自已通过 APP 抓包核对的
独立实现；不把真实节点凭据或私有抓包放进仓库。保留教育加速
`cmcc-auth-method: 0x80/0x82` 的原有测试。
