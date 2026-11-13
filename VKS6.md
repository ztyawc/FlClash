# VK S6 下载 TCP 出站

协议证据、线格式、源码接入点与后续开发边界见
[VK-S6-PORTING.md](VK-S6-PORTING.md)。

`type: vk-s6` 是 VK Android 4.4.16 下载线路中已抓包、已用独立客户端
复现的 IPv4 TCP 连接形式。mihomo 内核直接连接 VK 节点并转发 TCP，
不需要 Python SOCKS 中转或 APK 原生库。它不实现 VK 游戏 UDP 加速。

## 发布内核

从 `v0.1.2` 起，[GitHub Releases](https://github.com/ztyawc/mihomo-cn2/releases)
提供包含 VK S6 的 Ubuntu / Linux x86_64 静态内核；`v0.1.1` 尚不包含此协议。
下载、SHA256 校验与运行步骤见 [README.md](README.md)。发布前会执行
VK S6 的普通测试、gVisor 回归、竞态检测及示例配置校验。

Windows amd64 和 Android arm64 CLI 可从同一源码构建，常规 CI 做交叉编译
检查。上述 Linux 发布附件不能直接作为 Windows 可执行文件或 Android APK 使用。

## 配置

把以下节点放入 `proxies:`，填写自己当前会话的用户名和密码：

```yaml
- name: VK 下载华东
  type: vk-s6
  server: 61.172.165.150
  port: 8050
  port-end: 8150
  username: "CURRENT_SESSION_USERNAME"
  password: "CURRENT_SESSION_PASSWORD"
  udp: false
```

`server` 必须为数字 IPv4。`port` 是节点 TCP 端口范围的起点，
`port-end` 是终点；每条连接在闭区间内随机选择一个端口。
省略 `port-end` 时只用 `port`。节点地址和账号参数可能随 VK 会话变化。
`username`、`password` 是 VK 会话下发的原始字符串，内核完成线上的
固定异或处理。上述 IP 是一次实测的华东下载入口，不代表长期可用。
其他当时下发的专用下载入口有 `61.172.165.232`、`120.253.243.38`、
`211.95.36.213`；可分别写成代理节点，用代理组手动切换。

完整的可运行示例见 [examples/vks6.yaml](examples/vks6.yaml)。示例只把
Google/Cloudflare 测试域名交给 VK，其他流量直连；测试时按需修改规则。
个人凭据放在本地配置文件中，并限制文件读取权限；不要提交到仓库。

## 已实现的线格式

连接节点后发送 `06 | 用户名长度 | 异或用户名 | 密码长度 | 异或密码 |
09 01 | 目标 IPv4 | 目标端口`。鉴权字段使用固定 8 字节循环异或键
`3c 26 44 63 4c 2b 43 40`；成功应答为 `06 00`。之后每条 TCP 流
的双向数据逐字节异或 `5a`。目标域名经 mihomo DNS 解析为 IPv4，
内核识别出的 Fake-IP 地址会被拒绝，不会发送给节点。TCP 半关闭、
取消、连接关闭和短写由出站处理。

UDP、IPv6 目标、域名地址类型、其他 S6 命令和凭据自动更新没有实现。
节点可能限制目标或速度；`acc_p1_gz` 华南代理虽经测试能承载同一 TCP
报文，但不是 VK 标记的专用下载入口。

## 验证

```sh
go test ./transport/vks6 ./adapter ./adapter/outbound
go build -tags with_gvisor -trimpath -o bin/mihomo-vks6-linux-amd64 .
./bin/mihomo-vks6-linux-amd64 -t -f examples/vks6.yaml
```

实际测试时复制示例到被 `.gitignore` 排除的 `config.vks6.local.yaml`，
替换两项会话凭据并限制读取权限，然后运行：

```sh
cp examples/vks6.yaml config.vks6.local.yaml
chmod 600 config.vks6.local.yaml
# 编辑 config.vks6.local.yaml 中的 username / password
./bin/mihomo-vks6-linux-amd64 -f config.vks6.local.yaml
```

另一个终端经 mihomo 下载 1 MiB 测试数据：

```sh
curl --proxy http://127.0.0.1:7890 --noproxy '' \
  -o /dev/null -w 'HTTP %{http_code}, bytes %{size_download}, total %{time_total}s\n' \
  'https://speed.cloudflare.com/__down?bytes=1048576'
```

测试使用合成凭据和本地模拟节点。实际节点连通仍取决于当前账号的有效
会话参数；仓库内没有真实账号密码。Android arm64 CLI 可执行文件可通过
`CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -tags with_gvisor
-trimpath -o bin/mihomo-vks6-android-arm64 .` 构建。手机上的 mihomo
客户端需支持替换为这个定制内核；普通 mihomo 不能识别 `vk-s6`。

2026-11-13 使用 VK Android 4.4.16“暗区突围国际服·澳大利亚”的当前
S6 会话凭据，在 Windows amd64 内核和 `61.172.165.150:8050–8150`
上完成真实节点测试：经 mihomo 本地代理请求 Cloudflare 1 MiB 得到
HTTP 200、1,048,576 字节，用时 0.655 秒；请求 Google 下载文件前
64 KiB 得到 HTTP 206、65,536 字节，用时 0.501 秒。这是一次连通性
验证，不代表长期带宽；真实凭据仅存在仓库外的私有配置中。
