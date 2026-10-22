# Android special-edition signing

Published APKs use the fixed `flclash-special-release` key and retain the application ID
`com.follow.clash.dev`. The public certificate is `android/special-signing-certificate.pem`.
Its SHA-256 fingerprint is:

```text
0f0d56829143784914877c49aba73132ed176db5270acab7b338e4c470cf3835
```

The private PKCS12 keystore and its password are stored separately in the repository's
encrypted Actions Secrets, `SPECIAL_ANDROID_KEYSTORE` (base64) and
`SPECIAL_ANDROID_KEYSTORE_PASSWORD`. Both keystore passwords use that same password.
Keep the private backup outside the public repository and release assets. Restore these
same values when migrating the workflow; generating a replacement key breaks updates.

The special Android build and synchronization validation restore the key into ignored,
temporary files and set `FLCLASH_SPECIAL_EDITION=true`. Missing secrets or a certificate
mismatch stop the build. Every APK is verified with `apksigner` before it can be uploaded,
and each release includes `SIGNING_CERTIFICATE_SHA256`. Signing files are removed even
when validation fails. Upstream synchronization stops on source conflicts so Gradle
signing changes cannot silently replace the fork's configuration.

Versions signed with the earlier temporary debug keys require exporting configuration
and reinstalling once to switch to this fixed signer. Subsequent versions signed with
this key can update the installed app normally.
