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

This repository also includes QyProxy primary/CN2/CN2 download support.
See [README.md](README.md) for builds and [QYPROXY.md](QYPROXY.md) for its fields.

The built-in core upgrade is disabled to preserve the custom protocols.
This repository runs tests and a Windows cross-build in GitHub Actions; it does
not automatically merge upstream or publish releases.

Optional live tests use environment variables supplied locally. No live
credentials are stored in this repository or configured in its CI.
