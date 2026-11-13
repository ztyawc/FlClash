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

## Builds and updates

This repository also includes QyProxy primary/CN2/CN2 download, UU custom SNI,
and VK S6 download TCP support. See [README.md](README.md) for the feature matrix,
builds, protocol limits, and verification status.

The built-in core upgrade is disabled to preserve the custom protocols.
GitHub Actions runs tests and Windows/Android cross-builds. Version tags trigger
the multi-platform release workflow, including tests, Linux amd64 / Windows amd64 /
Android arm64 builds, Linux example configuration checks, and GZ/ZIP/SHA256SUMS
publication. Version v0.1.3 includes VK S6 alongside QyProxy, CMCC, and UU SNI on
all three platforms. Android artifacts are CLI executables, not APKs. Upstream
changes are not merged automatically.

Optional live tests use environment variables supplied locally. No live
credentials are stored in this repository or configured in its CI.
