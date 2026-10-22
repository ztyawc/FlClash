import argparse
import base64
import binascii
import hashlib
import os
from pathlib import Path
import re
import ssl
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parents[1]
ALIAS = 'flclash-special-release'
KEYSTORE = ROOT / 'android/app/keystore.jks'
PROPERTIES = ROOT / 'android/special-signing.properties'


def certificate_fingerprint():
    certificate = (ROOT / 'android/special-signing-certificate.pem').read_text()
    return hashlib.sha256(ssl.PEM_cert_to_DER_cert(certificate)).hexdigest()


def prepare():
    encoded = os.environ.get('SPECIAL_ANDROID_KEYSTORE', '')
    password = os.environ.get('SPECIAL_ANDROID_KEYSTORE_PASSWORD', '')
    if not encoded or not password:
        raise ValueError('Both fixed Android signing secrets are required; debug signing is disabled.')
    if not re.fullmatch(r'[a-zA-Z0-9_-]{16,128}', password):
        raise ValueError('The signing password must contain 16-128 letters, digits, underscores or hyphens.')
    try:
        data = base64.b64decode(encoded, validate=True)
    except binascii.Error:
        raise ValueError('SPECIAL_ANDROID_KEYSTORE is not valid base64.') from None
    with tempfile.TemporaryDirectory(prefix='flclash-signing-') as folder:
        candidate = Path(folder) / 'candidate.jks'
        candidate.write_bytes(data)
        candidate.chmod(0o600)
        result = subprocess.run([
            'keytool', '-exportcert', '-keystore', str(candidate), '-alias', ALIAS,
            '-storepass:env', 'SPECIAL_ANDROID_KEYSTORE_PASSWORD',
        ], capture_output=True)
        if result.returncode:
            raise ValueError('The fixed signing keystore could not be opened with its configured password.')
        expected = certificate_fingerprint()
        if hashlib.sha256(result.stdout).hexdigest() != expected:
            raise ValueError('The signing key does not match the pinned release certificate.')
    KEYSTORE.write_bytes(data)
    KEYSTORE.chmod(0o600)
    PROPERTIES.write_text(f'storePassword={password}\nkeyPassword={password}\nkeyAlias={ALIAS}\n')
    PROPERTIES.chmod(0o600)
    print(f'Fixed Android signing certificate SHA-256: {expected}')


def verify(apks):
    if not apks:
        raise ValueError('At least one APK is required for signature verification.')
    sdk = os.environ.get('ANDROID_HOME') or os.environ.get('ANDROID_SDK_ROOT')
    if not sdk:
        raise ValueError('ANDROID_HOME or ANDROID_SDK_ROOT must identify the Android SDK.')
    tools = list((Path(sdk) / 'build-tools').glob('*/apksigner'))
    if not tools:
        raise ValueError('Android build-tools apksigner is required.')
    apksigner = max(tools, key=lambda path: tuple(map(int, re.findall(r'\d+', path.parent.name))))
    expected = certificate_fingerprint()
    for apk in apks:
        result = subprocess.run([str(apksigner), 'verify', '--verbose', '--print-certs', str(apk)],
                                capture_output=True, text=True)
        if result.returncode:
            raise ValueError(f'APK signature verification failed: {apk.name}')
        fingerprints = re.findall(r'^Signer #\d+ certificate SHA-256 digest: ([0-9a-fA-F]+)$',
                                  result.stdout, re.MULTILINE)
        if [value.lower() for value in fingerprints] != [expected]:
            raise ValueError(f'APK does not use the fixed release certificate: {apk.name}')
        print(f'{apk.name}: fixed release certificate verified')
    (ROOT / 'dist/SIGNING_CERTIFICATE_SHA256').write_text(expected + '\n')


def clean():
    KEYSTORE.unlink(missing_ok=True)
    PROPERTIES.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description='Prepare and verify fixed special-edition APK signing.')
    parser.add_argument('command', choices=('prepare', 'verify', 'clean'))
    parser.add_argument('apks', nargs='*', type=Path)
    args = parser.parse_args()
    try:
        if args.command == 'prepare':
            prepare()
        elif args.command == 'verify':
            verify(args.apks)
        else:
            clean()
    except (ValueError, OSError) as error:
        parser.exit(1, f'{error}\n')


if __name__ == '__main__':
    main()
