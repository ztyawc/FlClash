# UU 自定义 SNI/TCP

`type: uusni` 是 UU Android 11.3.5 / versionCode 1595 下载分支的原生 Go
实现，范围为 **custom SNI、TCP、IPv4 节点、无外层 TLS**。它直接使用 mihomo
拨号器连接节点，不需要 UU、LSPosed、旧 `.so` 或额外本地代理进程。

## 填写参数

把下列条目放入配置的 `proxies:`，填写本人当前有效的参数。以下地址和混淆值
都是合成示例，不能作为实际线路使用：

```yaml
- name: UU 下载 SNI
  type: uusni
  server: 203.0.113.10
  port: 5556
  obfs-key: 77
  sni-crypt: true
  udp: false
```

| 字段 | 来源和要求 |
| --- | --- |
| `name` | 自定义节点名称，可填写游戏名 |
| `server` | 捕获的 `node_ip`；必须是数字 IPv4，不能填游戏下载域名 |
| `port` | 捕获的 `node_port`；整数 1–65535 |
| `obfs-key` | 捕获的 `obfs_key` / `sniXorKey`；整数 0–255，必须明确填写，0 是有效值 |
| `sni-crypt` | 捕获的 `sni_crypt`；必须明确填写 true/false 或 1/0 |
| `udp` | 仅 false；此协议分支不提供 UDP 或 UDP-over-TCP |
| `custom-sni` | 可省略，默认 true；false 会拒绝 |
| `outer-tls` / `tls` | 可省略，默认 false；true 会拒绝 |
| `network` | 可省略或填 tcp；其他传输会拒绝 |
| `dialer-proxy` | 可选，使用 mihomo 现有的链式拨号机制 |
| `interface-name` / `routing-mark` / `tfo` / `mptcp` | 使用现有内核拨号选项 |

IP 和混淆值由服务器下发，不根据 IP 计算。标准地址加密密钥由内核按节点
`IP:端口` 派生，无需再填密钥。若捕获结果标为 Hybrid、自定义密钥、非自定义
SNI 或 `outer_tls=true`，不属于当前适配范围，不能直接用上述条目冒充支持。

参数本身不构成节点长期可用或允许任意目标的保证；取参接口的账号会话获取
不在本内核中。节点到期后需用自己的有效配置更新参数。

## 分享链接和订阅

内核的 V2Ray 格式订阅转换器支持此前 v2rayNG 适配版和取参模块生成的链接：

```text
uusni://203.0.113.10:5556?obfs-key=77&sni-crypt=1#UU%20SNI
```

必需参数缺失、重复参数、外层 TLS、用户名密码、IPv6 节点及未知变体会拒绝。
Base64 编码的整个订阅同样可转换。客户端自身的“剪贴板导入”是否把链接传给
本内核取决于客户端实现；支持加载自定义 YAML 的客户端可直接使用上述配置。

## DNS、分流与目的地址

- 使用现有的代理组、规则、监听端口和 TUN 接入；示例见
  [examples/uusni.yaml](examples/uusni.yaml)。CN2、QyProxy 和 CMCC 配置保持原样。
- 每条 TCP 连接保留实际目标的域名/IPv4 和端口。游戏更新域名不是固定伪装
  SNI，也不会覆盖请求目标；目标端口 80 不会改成 443。
- Fake-IP 场景使用还原后的目标域名；显式 hosts/DNSMapping 选中的 IP 依照
  mihomo 的 `Metadata.Pure()` 语义保留，不修改调用方的元数据。
- 通过此节点的 DNS 应选 TCP DNS、DoH 或 DoT；UDP DNS 需要另外安排分流。
- `smux.enabled: true` 会拒绝。底层流没有 mux 或 UDP 扩展，也没有失败后尝试
  SProxy、直连或其他节点的内置分支；代理组按用户配置执行的切换仍由内核管理。

## 线格式与生命周期

XOR 前的包头为 `55 55 01 crypt target_port_BE16 field_length_u8 field`。
地址加密开启时，`field` 是 `10 || zero_IV_16 || AES-128-GCM(host) || tag_16`，
AAD 为空；AES key 为 ASCII `节点IP:端口`，截取前 16 字节或补 `U` 到 16 字节。
保留捕获样本的 16 字节全零 IV 是协议兼容行为，不应套用于新密码协议设计。

整个包头和之后的双向 TCP 数据均逐字节 XOR。没有独立的成功 ACK，后端不应
先读掉一段“状态响应”；原网站 HTTPS 仍由客户端和网站端到端协商和验证。
加密地址最多 222 字节，明文 ASCII 域名最多 253 字节，IPv6 目标明确拒绝。

实现处理短写、不修改调用方发送缓冲区、并发读写、TCP 半关闭、握手取消和
客户端关闭。拨号及包头写入受 10 秒上限或调用方更早的 deadline 限制；建立后
流的超时和转发由 mihomo 管理。复制路径禁止绕过 XOR 层，保留内核的 socket
保护、链式拨号、接口绑定、路由标记、统计和连接链记录。

## 验证

```sh
go test ./transport/uusni ./common/convert ./adapter ./adapter/outbound
go test -race ./transport/uusni
go test -race ./adapter ./adapter/outbound ./common/convert -run UUSNI
SKIP_CONCURRENT_TEST=1 SKIP_INTEROP_TEST=1 go test ./...
go test -tags with_gvisor ./transport/uusni ./transport/qyproxy ./transport/socks5 ./adapter ./adapter/outbound ./common/convert ./component/updater
```

合成测试覆盖独立密码向量、所有单字节 XOR 值、长度/字段边界、短写和缓冲区
不可变性、无 ACK 的服务端先发数据、取消/关闭、FIN 后仍可下载、配置与链接
转换、Fake-IP/hosts 语义、内核快速复制路径，以及通过本地模拟节点的严格
证书验证 HTTPS 大数据上传和下载。测试不使用用户节点或账号。

尚未验证真实 UU 节点互通、实际带宽、Android VPN/Root 模式和客户端界面。
此前旧 Android 代理使用原 `.so` 的成功实测不能替代本 Go 实现的真机测试。

本次使用 Go 1.26.8，通过上述目标包测试、竞态检查和 gVisor 回归；Linux
amd64、Windows amd64、Android arm64（CGO_ENABLED=0）均已编译，并通过
Linux CLI 的示例配置 `-t` 检查。Android 产物为使用 `/system/bin/linker64`
的 CLI 可执行文件；客户端内嵌库或 APK 需要沿用对应客户端的绑定构建流程。

完整回归在本次容器中有两项环境影响：已有 `TestMemoryInfo` 读取自身
`/proc/<pid>/statm` 时报告不存在；Sudoku 的 HTTP 隧道测试受环境 HTTP 代理
影响。两者均在原始提交复现。本地完整回归需仅对测试进程清除 HTTP(S)/ALL_PROXY
及其小写变量，并加 `-skip '^TestMemoryInfo$'`。仓库 CI 继续执行未跳过该内存
测试的 `go test ./...`，不把本地环境限制改成协议或生产代码的豁免。
