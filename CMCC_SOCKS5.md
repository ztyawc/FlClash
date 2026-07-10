# CMCC private SOCKS5 support

This fork adds the `0x80` and `0x82` private authentication methods described
by the CMCC education accelerator protocol. Client-to-server TCP streams and
complete SOCKS5 UDP datagrams are XOR-obfuscated with `0xff`; server responses
remain unchanged.

## Configuration

```yaml
proxies:
  - name: cmcc
    type: socks5
    server: 127.0.0.1
    port: 10800
    username: "your-username"
    password: "your-password"
    cmcc-auth-method: "0x80" # or "0x82"
    udp: true
```

When `cmcc-auth-method` is omitted, the outbound remains a standard SOCKS5
proxy. Credentials are never built into the core.

## Kernel releases

The [`Release CMCC kernels`](https://github.com/ztyawc/mihomo/actions/workflows/release-cmcc-kernels.yml)
workflow publishes only these two custom kernel assets:

- `mihomo-windows-amd64-v1-cmcc.zip` for Windows 10
- `mihomo-android-arm64-v8-cmcc.gz` for Android

The latest pair is available from the repository's
[`Releases`](https://github.com/ztyawc/mihomo/releases) page. Each release body
contains the embedded version, source commit, and SHA-256 checksums. GitHub also
shows its automatically generated source archives; those are not kernel builds.

## Upstream maintenance

MetaCubeX develops and releases mihomo from its `Alpha` branch. This fork keeps
`https://github.com/MetaCubeX/mihomo.git` as the `upstream` remote and carries
the private protocol changes on top of `upstream/Alpha`.

The fork's `main` branch is the maintenance and default branch, based on
upstream `Alpha`. The `Sync upstream Alpha` workflow
checks for updates every day, merges them, runs the full test suite plus
Windows amd64-v1 and Android arm64-v8 builds, and pushes only when every check
succeeds. Merge conflicts or test/build failures stop the workflow without
modifying the remote branch. After a successful synchronization, the release
workflow builds and publishes the new two-kernel pair from the latest `main`.

Manual synchronization uses the same safe merge flow:

```shell
git fetch upstream Alpha
git merge --no-edit upstream/Alpha
go test ./transport/socks5 ./adapter/outbound
git push origin HEAD:main
```

Optional live tests read connection details only from environment variables:

```shell
MIHOMO_CMCC_TEST_ADDR=host:port \
MIHOMO_CMCC_TEST_USERNAME=username \
MIHOMO_CMCC_TEST_PASSWORD=password \
MIHOMO_CMCC_TEST_METHOD=0x80 \
go test ./transport/socks5 ./adapter/outbound -run 'CMCCLive' -count=1
```
