# VK S6 下载 TCP 协议与 mihomo 移植手册

本文供后续开发者或 AI 独立阅读。协议结论来自 VK Android 4.4.16 的一次
下载会话抓包、独立 Python 客户端复现、定制 mihomo 实现和真实节点测试。
本文只记录线格式、实现位置与证据边界，**不包含用户当前会话的用户名、
密码、登录 token 或私有抓包**。

## 1. 当前结论与项目状态

- 已还原的是 **VK 下载节点的 IPv4/TCP S6 连接形式**。客户端发一条含
  会话用户名、密码和目标 IPv4/端口的请求，节点回复 `06 00` 后双向
  转发逐字节 `0x5a` 异或的数据。此处的“S6”来自客户端内部标识；
  不是公开的通用代理标准。
- 本仓库的 `type: vk-s6` 已在 Go 中原生实现，无需 Python 中转或 APK
  原生库。Linux、Windows amd64 和 Android arm64 CLI 均已编译。
- 本地模拟节点的端到端测试通过；2026-11-13 的 Windows 内核在真实
  华东下载节点上完成 Google 和 Cloudflare 下载，用户也自行测试成功。
- 这不是完整 VK VPN 移植。游戏 UDP/UDCP、IPv6、其他 S6 命令、
  服务端配置获取、账号自动续期尚未实现。
- 源码、测试和示例位于 `ztyawc/mihomo-cn2`。使用或继续开发时，应以
  对应版本的实际提交为准；会话凭据不随源码发布。

## 2. 证据层级

| 结论 | 证据 | 范围 |
|---|---|---|
| 请求与应答格式 | 手机 Wi-Fi 抓包：34 字节请求、2 字节成功应答；以当次会话参数重建，请求逐字节一致 | Android 4.4.16，“暗区突围国际服·澳大利亚”下载节点 TCP |
| 数据异或 | 抓到的 HTTP 请求与响应以 `0x5a` 解码后得到原始内容；独立客户端成功访问 HTTP/HTTPS | 已观察的 TCP 变体 |
| 独立互通 | 仓库外 `../vk_download_client.py` 只用 Python 标准库连接 VK 节点并完整下载测试数据 | 当次账号与节点配置 |
| mihomo 互通 | 本地模拟节点经完整 mihomo 代理链路下载 1,179,648 字节，HTTP 200，内容哈希一致；Windows 内核再测真实节点 | `vk-s6` IPv4/TCP 出站 |
| 区域入口 | 读取当前账号 31 款游戏、75 个区服的候选节点列表 | 当时下发给该账号的列表，非 VK 后台全量 |

原始分析记录在工作区外层的 `../VK_加速器_线路协议分析.md` 和
`../VK_下载协议本地实现与实测.md`。若只拿到本手册，上述证据范围和
以下线格式已足够理解或重新实现 TCP 出站；这些报告提供完整测速表。

## 3. 会话参数与下载入口

在已观察的 VK Android 加速会话中，应用传给 `startVpn` 的参数包含
`username`、`password`、`nodeData` 和 `gameData`。前两个是 **S6 节点
会话凭据**，不应当被解释为 VK 账号登录密码。`nodeData.download` 形如
`61.172.165.150:8050-8150`，分别提供接入 IP 和 TCP 端口范围。
当前抓包样本的用户名、密码 UTF-8 长度分别是 7 和 16 字节；实际值
随会话取得，不能放进源码或公开手册。

2026-11-12 在候选列表中看到的专用下载入口如下。名称中的区域是 VK
自己的标签；表格里的 IP 是**入口**，不能据此判断访问所有目标时的
最终出口或全程路径。

| 节点 ID | VK 标签 | 接入 IP |
|---:|---|---|
| 2 | `acc_download`，华东-手游CDN-1 | `61.172.165.232` |
| 129 | `acc_download`，华东-手游CDN-独服-电信 | `61.172.165.150` |
| 131 | `acc_download`，华东-手游CDN-独服-移动 | `120.253.243.38` |
| 1141 | `acc_download`，华东-手游CDN-独服-联通 | `211.95.36.213` |

75 个区服中，71 个下发相同的四个 `acc_download` 入口，4 个没有下发
下载入口；未发现标记为下载专用的华南或其他区域 IP。候选列表还包含
`acc_p1_gz` 华南代理 `14.29.123.16`、`121.14.34.180`、
`120.241.152.243`、`112.90.7.213`。华南 P1 入口实测也能接受同一
S6 TCP 报文并转发下载，但它们不是下载专用节点，实测速率与目标
可达性不同。把 `121.14.34.180` 当作下载入口时，手机上同一 4 MiB
Cloudflare 目标约 1.75–1.81 MB/s；当次华东专用入口约
5.89–6.53 MB/s。这些数值不是长期保底速度。

节点 UDP `7050` 仅用于当次节点延迟探测。它不是这里的 TCP 数据端口。
节点列表出现的 `socks5:26050` 服务字段也不能当作本协议的 TCP 数据端口。

## 4. 已验证的 TCP 线格式

外层先对选中的下载入口建立普通 TCP 连接，端口从 `8050–8150`
闭区间内为**每条新连接**选择。随后发送一次连接请求：

| 字段 | 大小 | 内容 |
|---|---:|---|
| 版本 | 1 字节 | `06` |
| 用户名长度 `Lu` | 1 字节 | 用户名 UTF-8 字节数，1–255 |
| 用户名 | `Lu` 字节 | 原始 UTF-8 字节逐字节异或固定键 |
| 密码长度 `Lp` | 1 字节 | 密码 UTF-8 字节数，1–255 |
| 密码 | `Lp` 字节 | 原始 UTF-8 字节逐字节异或固定键 |
| 命令、地址类型 | 2 字节 | 本次 TCP/IPv4 形式为 `09 01` |
| 目标 IP | 4 字节 | IPv4 网络序 |
| 目标端口 | 2 字节 | uint16，大端序 |

鉴权异或键为 `3c 26 44 63 4c 2b 43 40`，分别从下标 0 开始处理
用户名和密码：`encoded[i] = raw[i] XOR key[i mod 8]`。总请求长度为
`11 + Lu + Lp`；当次 `Lu=7`、`Lp=16`，合计 34 字节。节点成功
应答为**未异或**的 `06 00`。只读取这两个应答字节，紧随其后的字节
可能已经是目标服务器数据。

成功后，客户端到节点和节点到客户端的 TCP 载荷均逐字节异或 `5a`。
已观察的 TCP 数据流没有额外长度帧、计数器或逐包密钥。其核心逻辑：

```text
connect(node_ip, random_port_in_range)
send(06 | Lu | xor8(username) | Lp | xor8(password)
     | 09 01 | target_ipv4[4] | target_port_be16)
require(read_exactly(2) == 06 00)
for each direction: output_byte = input_byte XOR 5a
```

这是固定异或混淆，不提供密码学意义上的机密性；HTTPS 目标仍依靠
应用与目标站点之间的 TLS。没有观察到额外的时间戳、签名或独立 token
字段；这个结论限定于上述 TCP 请求，不外推到其他 S6/UDCP 变体。

**不含真实凭据的验证向量：**用户名 `vk-test`、密码
`0123456789abcdef`、目标 `43.174.102.54:80` 时，完整请求应为：

```text
06074a4d6917295837100c177650781e7577041f25012f4f262609012bae66360050
```

该向量由独立 Python `socket` / `struct` 计算，并写入
`transport/vks6/wire_test.go`。不得用真实会话请求报文替换公开向量。

## 5. 路由与 DNS 的区别

VK 原应用先用 Android `VpnService` 接管游戏流量，再依据当次下发的
`gameData.rules` 选择 `download`、`game` 或直连。候选节点列表里有
`acc_download`，不表示当前游戏会话必定启用下载角色。一个明确例子：
“三角洲行动国际服·中国香港”的候选列表有四个下载节点，实际运行时
`nodeData` 没有 `download`，`download.dgameglobal.com` 当次规则为直连。
相反，“暗区突围国际服·澳大利亚”曾把
`gcloud.arenabreakout.com` 指向 `download`，解析所得目标 IP 的 TCP
连接进入下载节点。域名含有 `download` 字样本身不构成路由规则。

当前 mihomo 出站只负责**已被 mihomo 规则选中的 TCP 连接**。S6 请求
仅实现目标 IPv4 类型，因此域名必须先解析成真实 A 地址。适配器使用
`Metadata.Pure()` 保留 mihomo 显式 hosts/DNS 映射的选择，再通过内核
解析器取得 IPv4；若没有原始域名却被标记为 Fake-IP，或解析结果被
内核识别为 Fake-IP，就返回错误。实际部署仍需配置可信的真实 DNS；
外部系统若返回未被 mihomo Fake-IP 池识别的合成地址，也可能导致失败。
WSL 的旧测试环境曾把公网域名解析到 `198.18.0.0/15`，因此独立
Python 客户端用了 HTTPS DNS。示例配置使用 DoH。

Cloudflare Trace 曾显示 `loc=HK`、`colo=HKG`，说明**该次 Cloudflare
请求**被香港 Cloudflare 机房处理且出口被识别为香港；不能由此推断
所有目标都使用同一出口或 VK 接入后的完整物理路由。

## 6. 本仓库的原生移植位置

本仓库是 `ztyawc/mihomo-cn2` 的定制 mihomo。普通 mihomo 仅靠
YAML 不认识 `vk-s6`，需要包含这些源码改动的自编译内核。

| 文件 | 职责 |
|---|---|
| `transport/vks6/wire.go` | 校验入口 IP、端口、UTF-8 凭据；编码 IPv4 TCP 请求 |
| `transport/vks6/client.go` | 逐连接选端口、mihomo 拨号、10 秒建连/握手上限、读取成功应答、取消与关闭 |
| `transport/vks6/conn.go` | 双向 XOR `net.Conn`，短写、半关闭、并发读写；禁止快速复制绕过 XOR |
| `adapter/outbound/vks6.go` | `VKS6Option`、目标 IPv4 解析、`DialContext`、代理链信息、关闭 |
| `adapter/parser.go` | 识别 `type: vk-s6`，拒绝 `smux` |
| `constant/adapters.go` | 追加 `VKS6` 类型枚举和显示名称，不改变旧类型编号 |
| `adapter/outbound/util.go`、`uusni.go` | 抽出严格整数解析，避免弱类型配置将小数端口截断；保留 UU 行为 |
| `transport/vks6/*_test.go`、`adapter/vks6_test.go`、`adapter/outbound/vks6_test.go` | 合成向量、模拟节点、配置边界与 DNS 元数据测试 |

实现时应守住以下条件：

1. **拨号器走 mihomo 原有 `BasicOption.NewDialer`**，以保留接口绑定、
   路由标记、链式代理、API 测试拨号器等能力。连接返回 `NewConn`。
2. **线上的用户名和密码在头内分别从密钥下标 0 开始异或**；数据阶段
   只用 `0x5a`。不能把标准 SOCKS5 鉴权封包或密码学握手套到此处。
3. **短写必须继续写完，写入不得修改调用方缓冲区**。`Read` 解码
   已收到的字节；`CloseWrite`/`CloseRead` 传递给底层 TCP。
4. **禁用 ReaderReplaceable/WriterReplaceable**，否则 mihomo 的复制
   优化可能越过异或层。成功应答只消费两个字节。
5. **连接建立阶段受上下文取消和 10 秒上限约束**；建立成功后，
   原始建连上下文结束不应关闭传输中的 TCP 流。适配器关闭时终止
   待建连和已建立连接。
6. **配置严格校验**：数字 IPv4 入口、1–65535 端口、`port-end >= port`、
   1–255 UTF-8 字节的非空用户名和密码。`port-end` 省略时只用 `port`。
   `udp: true`、外层 TLS、非 TCP 网络、IPv6 目标、`smux` 不在本实现范围。
7. **没有静默直连降级**。节点拒绝、凭据过期或 DNS 失败应返回错误；
   如需切换节点，由用户的 mihomo 代理组与规则控制。

## 7. 配置、构建与测试

`examples/vks6.yaml` 是不含真实凭据的完整示例；`VKS6.md` 是较短的
使用说明。自定义节点的核心字段如下：

```yaml
- name: VK 下载华东
  type: vk-s6
  server: 61.172.165.150 # 当次下发值，使用前核对
  port: 8050
  port-end: 8150
  username: "CURRENT_SESSION_USERNAME"
  password: "CURRENT_SESSION_PASSWORD"
  udp: false
```

示例规则只将 `dl.google.com` 和 `speed.cloudflare.com` 送入该节点，
其余直连；不会自动复刻 VK 针对每个游戏的动态规则。用户当前 Windows
私人测试配置将本地代理端口设为 `20891`；公开示例使用 `7890`。

在本仓库根目录执行：

```sh
go test ./transport/vks6 ./adapter ./adapter/outbound
CGO_ENABLED=1 go test -race ./transport/vks6 ./adapter ./adapter/outbound
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1 \
  go build -tags with_gvisor -trimpath -o bin/mihomo-vks6-linux-amd64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=v1 \
  go build -tags with_gvisor -trimpath -o bin/mihomo-vks6-windows-amd64.exe .
CGO_ENABLED=0 GOOS=android GOARCH=arm64 \
  go build -tags with_gvisor -trimpath -o bin/mihomo-vks6-android-arm64 .
./bin/mihomo-vks6-linux-amd64 -t -f examples/vks6.yaml
```

本地使用 Go 1.26.8。完整回归曾通过；该环境中的既有
`TestMemoryInfo` 读取 `/proc/<pid>/statm` 会失败，故使用
`go test ./... -skip '^TestMemoryInfo$'`，并为既有 Sudoku 测试清除
HTTP(S)/ALL_PROXY 环境变量。这个跳过与 VK 协议实现无关。

已经完成的实测：

| 测试 | 结果 | 解释 |
|---|---|---|
| 本地模拟 S6 节点 + 完整 mihomo 代理链路 | HTTP 200，1,179,648 字节，SHA-256 与源数据一致 | 验证编码、解码、代理接入和双向流 |
| Windows amd64 内核 + `61.172.165.150` + 当前会话凭据，Cloudflare 1 MiB | HTTP 200，1,048,576 字节，0.655 秒 | 真实节点连通样本，不是保底速率 |
| 同次 Windows 实测，Google Range 64 KiB | HTTP 206，65,536 字节，0.501 秒 | 真实节点连通样本 |
| 用户自行在 Windows 复测 | 用户反馈“可以的” | 用户环境确认可用，未提供量化样本 |
| Android arm64 | CLI 编译通过 | 尚无本次 Android 内核真机互通结论 |

Windows/Android 构建产物在本地 `bin/`，该目录被 `.gitignore` 排除；
Android 文件是 CLI 可执行程序，不是 APK 或客户端内嵌库。手机 GUI
客户端必须支持加载此定制 mihomo 核心并保留 `vk-s6` 配置字段。

## 8. 未覆盖范围与后续工作

- **凭据与节点发现：**首版手填当前会话的 `username/password` 和入口；
  不会调用 VK 账号登录接口，也不会自动更新。当前私有凭据、Windows
  测试配置保存在仓库外；分享本手册时无需附带这些文件。会话过期后需
  从用户自己的 VK 会话重新获取，并替换配置。
- **其他协议：**UDP/UDCP、IPv6、域名地址类型、`09 01` 以外的命令、
  异常应答的完整语义、密钥轮换和完整游戏 VPN 规则仍需独立抓包验证。
- **线路选择：**当前入口范围随会话下发。华南 P1 虽能跑相同 TCP
  报文，速度与目的站点可达性不同；不要依据入口延迟推断下载带宽。
- **分发：**本仓库只提交源码、合成测试与公开文档；私有会话凭据
  不随源码或构建产物分发。
