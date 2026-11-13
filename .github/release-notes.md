适用于 Ubuntu / Linux x86_64 的 mihomo-cn2 定制内核，由 GitHub Actions 构建并发布。

## 支持的专用协议

| 协议 | 配置方式 | 范围 |
| --- | --- | --- |
| QyProxy / CN2 | `type: qyproxy` | `primary` / `cn2` / `cn2_download`，IPv4 TCP/UDP、认证、心跳、隧道内 DNS；不支持 UDP 分片 |
| CMCC 教育加速 | `type: socks5` + `cmcc-auth-method` | `0x80` / `0x82` 私有认证和 TCP/UDP 混淆 |
| UU 自定义 SNI | `type: uusni` | IPv4 下载 TCP、custom-SNI、无外层 TLS；支持 `uusni://` 转换，不支持 UDP |
| VK S6 下载 | `type: vk-s6` | IPv4/TCP、当前会话用户名密码、可配置端口范围；不支持游戏 UDP、IPv6 或凭据自动更新 |

从 v0.1.2 起，Linux 发布内核加入 VK S6；此前 v0.1.1 不包含 VK S6。
四类专用节点均由 Go 原生实现，账号、节点和会话参数需自行提供。

## 下载和运行

- 附件为 Linux amd64，使用 `GOAMD64=v1`、`with_gvisor`、`CGO_ENABLED=0` 静态编译。
- 下载 `.gz` 与 `SHA256SUMS`，在同一目录执行 `sha256sum -c SHA256SUMS`。
- 使用 `gzip -d` 解压，将文件重命名为 `mihomo`，运行 `chmod +x mihomo`。
- 用 `./mihomo -v` 查看版本，再使用 `./mihomo -f config.yaml` 启动。
- 本地代理可监听 `127.0.0.1:7890`，HTTP / SOCKS5 共用端口；全局模式通过只包含目标节点的 `GLOBAL` 组选择出口。
- 私有仓库下载需要登录有权限的 GitHub 账号。

## 验证和边界

发布前通过完整 Go 测试、四类专用协议的 gVisor 回归、UU SNI / VK S6
竞态检测、Linux 可执行文件启动及 QyProxy / UU SNI / VK S6 示例配置校验。

QyProxy 已有 Linux / Windows 的 TCP/UDP 测试记录；VK S6 已有 Windows
真实节点下载记录。UU SNI 的 Go 实现目前通过模拟节点和本地 HTTPS 测试，
真实 UU 节点互通尚未验证。Android CLI 编译检查不代表 Android 真机验证。
协议限制、会话有效期和客户端接入方式以对应说明为准。

内置核心升级已禁用，更新时请使用本仓库的定制构建。
