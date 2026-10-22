# mihomo-cn2

支持 QyProxy 游戏主线、CN2、CN2 下载和 UU 自定义 SNI/TCP 的定制 mihomo
内核，同时保留 CMCC SOCKS5 `0x80` / `0x82` 支持。

## 功能与配置

- `type: qyproxy`：支持 `primary`、`cn2`、`cn2_download` 三种角色。
- Go 原生实现 CHAP-MD5 认证、心跳、TCP/UDP 数据转发和隧道内 DNS。
- 无需运行 Python 中转或原 APP；节点账号和配置须自行提供。
- 保留 mihomo 的代理组、规则、TUN 等功能。
- `type: uusni`：原生 UU 下载自定义 SNI/TCP，无外层 TLS。支持手填节点参数
  和 `uusni://` 订阅转换，不需要 UU、LSPosed、旧原生库或本地中转进程。
  参数说明和限制见 [UUSNI.md](UUSNI.md)，配置见
  [examples/uusni.yaml](examples/uusni.yaml)。

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

本次适配使用 Go 1.26.8 验证。保留原 Go module 路径以兼容内部导入。

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

客户端需支持加载自定义 mihomo 内核并保留 `qyproxy` 字段。内置核心自动升级
已禁用，防止被不支持这些协议的官方内核替换。

## 验证

Linux、Windows 三条线路已通过 HTTP、HTTPS、UDP DNS、UDP STUN、并发及心跳测试。
Android 已编译，尚未真机验证。测试夹具使用合成账号和本地模拟服务器。

UU SNI 另外覆盖独立线格式向量、短写、取消/关闭、FIN、域名恢复、URI 转换、
内核复制路径和严格证书验证的本地 HTTPS；真实 UU 节点及客户端仍需实测，
具体结果和环境限制见 [UUSNI.md](UUSNI.md)。

```sh
SKIP_CONCURRENT_TEST=1 SKIP_INTEROP_TEST=1 go test ./...
go test -tags with_gvisor ./transport/uusni ./transport/qyproxy ./transport/socks5 ./adapter ./adapter/outbound ./common/convert ./component/updater
```

GitHub Actions 在 main 的 push、PR 和手动触发时运行测试及 Windows 交叉编译。
当前不自动同步上游、不自动发布 Release。

## 来源和许可

基于 [ztyawc/mihomo](https://github.com/ztyawc/mihomo) 的提交
`a72cf3ec7d034b28644ed7447032bd731e3bfa64`，在其上加入 QyProxy 适配。
原始上游为 [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo)。

本仓库以独立初始提交导入源码，保留来源说明、版权及许可文件。
遵循 [LICENSE](LICENSE)，原项目说明与附加声明见
[README.upstream.md](README.upstream.md)。
