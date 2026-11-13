# mihomo-cn2

在 mihomo 基础上原生接入 QyProxy、CMCC 教育加速、UU 自定义 SNI 和
VK S6 下载节点的定制内核。支持 HTTP / SOCKS5 本地代理、代理组、规则
和 TUN 接入；各专用协议的实现范围见下表。

## 功能与配置

| 专用协议 | 配置入口 | 已实现能力 | 主要边界 |
| --- | --- | --- | --- |
| [QyProxy / CN2](QYPROXY.md) | `type: qyproxy` | `primary`、`cn2`、`cn2_download`；CHAP-MD5 认证、心跳、TCP/UDP、隧道内 DNS | 仅 IPv4；不支持 UDP 分片、QyVpn 或 Hop |
| [CMCC 教育加速](CMCC_SOCKS5.md) | `type: socks5` + `cmcc-auth-method` | 私有认证 `0x80` / `0x82`、TCP/UDP 混淆 | 需要匹配的 CMCC 节点与认证参数 |
| [UU 自定义 SNI](UUSNI.md) | `type: uusni` | UU 下载 custom-SNI/TCP、`uusni://` 链接和订阅转换 | 仅 IPv4/TCP、无外层 TLS 的特定分支；不支持 UDP 或其他 UU 变体 |
| [VK S6 下载](VKS6.md) | `type: vk-s6` | 原生 IPv4/TCP、当前会话用户名密码、端口范围随机连接 | 不支持游戏 UDP、IPv6、其他 S6 命令或会话凭据自动更新 |

以上连接和转发均由 Go 内核完成，无需额外 Python 中转或原 APP 常驻。
节点、账号和当前会话参数须自行提供，内核不负责账号登录、取参或续期。
TUN 是流量接入方式，不会给仅支持 TCP 的出站增加 UDP 能力。

配置示例：[QyProxy 三线路](examples/qyproxy.yaml)、[UU SNI](examples/uusni.yaml)、
[VK S6](examples/vks6.yaml)。VK 的协议证据和移植细节见
[VK-S6-PORTING.md](VK-S6-PORTING.md)。

[三线路示例配置](examples/qyproxy.yaml) 中的账号、密码、SN 均为占位内容。
下列节点放在配置的 `proxies:` 下，替换为自己当前有效的参数：

```yaml
  - name: CN2 下载
    type: qyproxy
    role: cn2_download
    server: 119.147.181.52
    port: 7025
    username: "填写CN2下载用户名"
    password: "填写CN2专用secret"
    sn: "填写设备SN"
    game-id: 19535
    server-id: 21254
    game-area: hk
    zone: hk
```

用户名填写原始节点用户名，内核自动拼接认证后缀；主线密码和 CN2 secret
来源不同，不能混用。三种角色默认支持 UDP，当前仅支持 IPv4 和未分片的协议
UDP 数据包。游戏 ID、区服 ID、区域和节点地址须以自己的有效配置为准。

完整字段和协议限制见 [QYPROXY.md](QYPROXY.md)，教育加速字段见
[CMCC_SOCKS5.md](CMCC_SOCKS5.md)。官方 mihomo、v2rayNG 等其他内核
不能直接加载 `type: qyproxy`。

## 构建和运行

### 版本与下载

`v0.1.3` 的 Linux、Windows 和 Android 发布内核加入 VK S6，包含上表四类专用协议。
此前的 `v0.1.1` 包含 QyProxy、CMCC 和 UU SNI；需要 VK S6 时使用
`v0.1.3` 或更新的定制构建。

从 [Releases](https://github.com/ztyawc/mihomo-cn2/releases) 按系统选择附件，
同时下载对应的 `SHA256SUMS`：

| 系统 | 架构 | v0.1.3 附件 | 使用方式 |
| --- | --- | --- | --- |
| Ubuntu / Linux | x86_64 / amd64 | `mihomo-cn2-linux-amd64-v0.1.3.gz` | 解压、添加执行权限，直接运行或作为 systemd 服务 |
| Windows | x86_64 / amd64 | `mihomo-cn2-windows-amd64-v0.1.3.zip` | 解压得到 `mihomo.exe`，直接运行或替换客户端内核 |
| Android | arm64 | `mihomo-cn2-android-arm64-v0.1.3.gz` | 解压后作为 CLI 内核运行或交给支持替换内核的客户端；不是 APK |

三个平台均启用 `with_gvisor` 和 `CGO_ENABLED=0`，Linux / Windows amd64
使用 v1 指令集。Linux 附件为静态可执行文件。
仓库为私有时，需登录有权限的 GitHub 账号下载附件；也可通过已登录的
GitHub CLI 下载：

```sh
gh release download v0.1.3 --repo ztyawc/mihomo-cn2 \
  -p mihomo-cn2-linux-amd64-v0.1.3.gz -p SHA256SUMS
```

以下以 Ubuntu / Linux x86_64 为例，在下载目录校验并运行：

```sh
sha256sum --ignore-missing -c SHA256SUMS
gzip -dk mihomo-cn2-linux-amd64-v0.1.3.gz
mv mihomo-cn2-linux-amd64-v0.1.3 mihomo
chmod +x mihomo
./mihomo -v
# 准备 config.yaml，填入自己的节点参数
./mihomo -f config.yaml
```

Windows 解压 ZIP 后可执行 `mihomo.exe -v` 和 `mihomo.exe -f config.yaml`；
也可用 PowerShell 的 `Get-FileHash <附件路径> -Algorithm SHA256` 对照
`SHA256SUMS` 中对应附件的哈希。Android 需要相容的运行环境或客户端接入，
Android CLI 构建通过不等于真机或 APK 验证。

### 从源码构建

项目使用 Go 1.26 系列工具链，本地适配曾使用 Go 1.26.8 验证。
GitHub Actions 使用 MetaCubeX 的 Go 1.26 工具链镜像，实际补丁版本记录在
各 Release 的构建信息中。保留原 Go module 路径以兼容内部导入。

```sh
git clone https://github.com/ztyawc/mihomo-cn2.git
cd mihomo-cn2
go build -tags with_gvisor -trimpath -o bin/mihomo .
cp examples/qyproxy.yaml config.yaml
# 编辑 config.yaml，填入自己的节点参数
./bin/mihomo -f config.yaml
```

Linux 上交叉编译 Windows amd64：

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=v1 go build -tags with_gvisor -trimpath -o bin/mihomo.exe .
```

Android arm64 CLI 可使用纯 Go 交叉编译，本次 UU SNI 已验证此方式：

```sh
CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -tags with_gvisor -trimpath -o bin/mihomo-android-arm64 .
```

需要启用 CGO 时，也可使用此前验证的 NDK r29-beta1 / API 34 编译器：

```sh
CGO_ENABLED=1 GOOS=android GOARCH=arm64 CC="${ANDROID_NDK_HOME}/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android34-clang" go build -tags with_gvisor -trimpath -o bin/mihomo-android-arm64 .
```

客户端需支持加载自定义 mihomo 内核并保留专用协议字段。内置核心自动升级
已禁用，更新时应使用本仓库的定制构建。

### VPS 本地全局代理

保留配置中的 `proxies:` 节点参数后，可将本地入口与全局代理组配置为：

```yaml
mixed-port: 7890
allow-lan: false
bind-address: 127.0.0.1
mode: global
ipv6: false

proxy-groups:
  - name: GLOBAL
    type: select
    proxies:
      - CN2 下载 # 替换为自己 proxies 中的节点名称
```

HTTP 和 SOCKS5 共用 `127.0.0.1:7890`，所有接入该端口的请求均选择指定节点。
程序需显式使用代理；例如：

```sh
curl --noproxy '' --proxy socks5h://127.0.0.1:7890 \
  --connect-timeout 10 --max-time 30 https://api.ipify.org
```

需要后台运行和开机自启时，可使用 systemd。普通本地代理模式无需开放公网
7890 端口，也无需启用 TUN 或修改整机路由。

## 验证

| 协议 | 已有验证记录 | 尚需验证的范围 |
| --- | --- | --- |
| QyProxy | Linux / Windows 三角色的 HTTP、HTTPS、UDP DNS、UDP STUN、并发和心跳测试 | Android 真机与各自当前节点参数 |
| CMCC | 私有认证、TCP/UDP 混淆及本地模拟节点回归 | 各自当前节点和客户端环境 |
| UU SNI | 线格式、短写、取消/关闭、FIN、域名恢复、订阅转换、复制路径和本地严格证书 HTTPS | 真实 UU 节点互通、实际带宽、Android VPN/Root 模式 |
| VK S6 | 线格式和模拟节点回归；2026-11-13 Windows 内核真实节点的 Google / Cloudflare 下载记录 | 当前会话有效性、其他节点和 Android 真机 |

真实节点的单次下载记录不代表长期带宽或所有目标都可访问。测试夹具使用
合成账号和本地模拟服务器；完整证据及环境边界见对应协议说明。

```sh
SKIP_CONCURRENT_TEST=1 SKIP_INTEROP_TEST=1 go test ./...
go test -tags with_gvisor ./transport/vks6 ./transport/uusni ./transport/qyproxy ./transport/socks5 ./adapter ./adapter/outbound ./common/convert ./component/updater
go test ./transport/vks6 ./adapter ./adapter/outbound
```

GitHub Actions 在 main 的 push、PR 和手动触发时运行测试及 Windows / Android
交叉编译，并对 UU SNI / VK S6 执行竞态检测。推送
`v<主版本>.<次版本>.<修订版本>` 标签时，发布流程会运行完整 Go 测试、四类
专用协议的 gVisor 回归及 UU SNI / VK S6 竞态检测，再从同一源码提交并行构建
Linux amd64、Windows amd64 和 Android arm64 内核。Linux 构建会校验
QyProxy、UU SNI 和 VK S6 示例配置；所有平台成功后统一生成 GZ / ZIP
和 SHA256SUMS，发布到同一个 GitHub Release。
也可手动运行 `Release Linux, Windows and Android`，填写尚未发布 Release
的现有版本标签。
当前不自动同步上游。

## 来源和许可

基于 [ztyawc/mihomo](https://github.com/ztyawc/mihomo) 的提交
`a72cf3ec7d034b28644ed7447032bd731e3bfa64`，在其上加入 QyProxy 适配。
原始上游为 [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo)。

本仓库以独立初始提交导入源码，保留来源说明、版权及许可文件。
遵循 [LICENSE](LICENSE)，原项目说明与附加声明见
[README.upstream.md](README.upstream.md)。
