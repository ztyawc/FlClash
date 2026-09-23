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

The embedded version is `<upstream release tag>-cmcc.<commit>`, for example
`v1.19.31-cmcc.0123456789ab`. Pushes that only touch Markdown, `docs/` or
workflow files do not publish a release because the kernels would be identical;
run the workflow manually (with `force` to replace an existing release) when a
CI change needs a fresh build.

The built-in core upgrade (`POST /upgrade`, the "Upgrade Core" button in
dashboards) is disabled in these kernels. It would otherwise replace them with
an official build that lacks CMCC support. Download new kernels from the
Releases page instead; UI and GEO database updates still work.

## Upstream maintenance

MetaCubeX develops and releases mihomo from its `Alpha` branch. This fork keeps
`https://github.com/MetaCubeX/mihomo.git` as the `upstream` remote and carries
the private protocol changes on top of `upstream/Alpha`.

The fork's `main` branch is the maintenance and default branch, based on
upstream `Alpha`. The `Sync upstream Alpha` workflow
checks for updates on the first day of every month (or when run manually),
merges them, runs the full test suite plus
Windows amd64-v1 and Android arm64-v8 builds, and pushes only when every check
succeeds. Merge conflicts or test/build failures stop the workflow without
modifying the remote branch. After a successful synchronization, the release
workflow builds and publishes the new two-kernel pair from the latest `main`.

The Actions token cannot push workflow files, so the automated merge keeps this
fork's `.github/workflows` and lists any skipped upstream workflow changes in the
run summary. The test suite includes an in-process CMCC server that drives the
socks5 outbound end to end, so a merge that breaks the CMCC data path fails
before anything is pushed.

Manual synchronization uses the same safe merge flow:

```shell
git fetch upstream Alpha
git merge --no-edit upstream/Alpha
go test ./transport/socks5 ./adapter/outbound ./component/updater
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
