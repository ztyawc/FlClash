import base64
import hashlib
import importlib.util
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location('special_signing', ROOT / 'tool/special_android_signing.py')
SIGNING = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SIGNING)


class SpecialSigningTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fixture = tempfile.TemporaryDirectory(prefix='flclash-signing-fixture-')
        cls.addClassCleanup(cls.fixture.cleanup)
        folder = Path(cls.fixture.name)
        cls.password = 'fixture-password-for-tests'
        key = folder / 'release.jks'
        env = {**os.environ, 'TEST_SIGNING_PASSWORD': cls.password}
        subprocess.run([
            'keytool', '-genkeypair', '-noprompt', '-storetype', 'PKCS12',
            '-keystore', str(key), '-alias', SIGNING.ALIAS,
            '-storepass:env', 'TEST_SIGNING_PASSWORD', '-keypass:env', 'TEST_SIGNING_PASSWORD',
            '-keyalg', 'RSA', '-keysize', '2048', '-validity', '30', '-dname', 'CN=Test Fixture',
        ], env=env, capture_output=True, check=True)
        command = ['keytool', '-exportcert', '-keystore', str(key), '-alias', SIGNING.ALIAS,
                   '-storepass:env', 'TEST_SIGNING_PASSWORD']
        cls.certificate = subprocess.check_output([*command, '-rfc'], env=env)
        cls.fingerprint = hashlib.sha256(subprocess.check_output(command, env=env)).hexdigest()
        cls.keystore = key.read_bytes()

    def setUp(self):
        folder = tempfile.TemporaryDirectory(prefix='flclash-signing-test-')
        self.addCleanup(folder.cleanup)
        self.root = Path(folder.name)
        (self.root / 'android/app').mkdir(parents=True)
        (self.root / 'dist').mkdir()
        (self.root / 'android/special-signing-certificate.pem').write_bytes(self.certificate)
        self.keystore = self.root / 'android/app/keystore.jks'
        self.properties = self.root / 'android/special-signing.properties'
        self.enterContext(mock.patch.multiple(SIGNING, ROOT=self.root, KEYSTORE=self.keystore,
                                              PROPERTIES=self.properties))
        self.enterContext(mock.patch.dict(os.environ, {
            'SPECIAL_ANDROID_KEYSTORE': base64.b64encode(type(self).keystore).decode(),
            'SPECIAL_ANDROID_KEYSTORE_PASSWORD': self.password,
        }))

    def test_prepare_uses_the_pinned_key_and_cleanup_preserves_sdk_properties(self):
        local = self.root / 'android/local.properties'
        local.write_text('sdk.dir=/fixture/android-sdk\n')
        SIGNING.prepare()
        self.assertEqual(self.keystore.read_bytes(), type(self).keystore)
        self.assertEqual(self.keystore.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.properties.stat().st_mode & 0o777, 0o600)
        self.assertIn(f'keyAlias={SIGNING.ALIAS}\n', self.properties.read_text())
        SIGNING.clean()
        SIGNING.clean()
        self.assertFalse(self.keystore.exists())
        self.assertFalse(self.properties.exists())
        self.assertEqual(local.read_text(), 'sdk.dir=/fixture/android-sdk\n')

    def test_missing_corrupt_or_incorrect_credentials_fail_without_debug_fallback(self):
        for name, value in (('SPECIAL_ANDROID_KEYSTORE', ''),
                            ('SPECIAL_ANDROID_KEYSTORE', 'invalid base64!'),
                            ('SPECIAL_ANDROID_KEYSTORE_PASSWORD', ''),
                            ('SPECIAL_ANDROID_KEYSTORE_PASSWORD', 'incorrect-password')):
            with self.subTest(name=name, value=value), mock.patch.dict(os.environ, {name: value}):
                with self.assertRaises(ValueError):
                    SIGNING.prepare()
                self.assertFalse(self.keystore.exists())
                self.assertFalse(self.properties.exists())

    def test_a_different_certificate_cannot_replace_the_fixed_signer(self):
        shutil.copyfile(ROOT / 'android/special-signing-certificate.pem',
                        self.root / 'android/special-signing-certificate.pem')
        with self.assertRaisesRegex(ValueError, 'pinned release certificate'):
            SIGNING.prepare()
        self.assertFalse(self.keystore.exists())

    def verify(self, output, status=0):
        signer = self.root / 'sdk/build-tools/36.0.0/apksigner'
        signer.parent.mkdir(parents=True, exist_ok=True)
        signer.write_text(f'#!/bin/sh\nprintf "%s\\n" {shlex.quote(output)}\nexit {status}\n')
        signer.chmod(0o755)
        apks = [self.root / 'dist' / name for name in ('arm64.apk', 'arm32.apk')]
        for apk in apks:
            apk.touch()
        with mock.patch.dict(os.environ, {'ANDROID_HOME': str(self.root / 'sdk')}):
            SIGNING.verify(apks)

    def test_verified_apks_record_the_fixed_fingerprint(self):
        self.verify(f'Signer #1 certificate SHA-256 digest: {self.fingerprint}')
        self.assertEqual((self.root / 'dist/SIGNING_CERTIFICATE_SHA256').read_text(),
                         self.fingerprint + '\n')

    def test_versioned_signer_labels_record_the_same_fixed_certificate(self):
        self.verify('\n'.join([
            f'V2 Signer: certificate SHA-256 digest: {self.fingerprint}',
            'V3.0 Signer: (minSdkVersion=28, maxSdkVersion=2147483647) '
            f'certificate SHA-256 digest: {self.fingerprint.upper()}',
            f'Source Stamp Signer: certificate SHA-256 digest: {"0" * 64}',
        ]))
        self.assertEqual((self.root / 'dist/SIGNING_CERTIFICATE_SHA256').read_text(),
                         self.fingerprint + '\n')

    def test_real_android_tools_verify_the_fixed_signer(self):
        sdk = os.environ.get('ANDROID_HOME') or os.environ.get('ANDROID_SDK_ROOT')
        if not sdk:
            self.skipTest('Android SDK is required for real APK signing verification')
        tools = sorted((Path(sdk) / 'build-tools').glob('*/apksigner'),
                       key=lambda path: tuple(map(int, re.findall(r'\d+', path.parent.name))))
        platform = next((Path(sdk) / 'platforms').glob('*/android.jar'), None)
        if not tools or not platform or not tools[-1].with_name('aapt').is_file():
            self.skipTest('Android build-tools and an SDK platform are required')
        signer = tools[-1]
        manifest = self.root / 'AndroidManifest.xml'
        manifest.write_text('<manifest xmlns:android="http://schemas.android.com/apk/res/android" '
                            'package="com.flclash.signingtest">'
                            '<uses-sdk android:minSdkVersion="24"/><application/></manifest>')
        unsigned = self.root / 'unsigned.apk'
        subprocess.run([str(signer.with_name('aapt')), 'package', '-f', '-M', str(manifest),
                        '-I', str(platform), '-F', str(unsigned)], capture_output=True, check=True)
        SIGNING.prepare()
        apk = self.root / 'dist/real-signed.apk'
        subprocess.run([str(signer), 'sign', '--ks', str(self.keystore),
                        '--ks-key-alias', SIGNING.ALIAS,
                        '--ks-pass', 'env:SPECIAL_ANDROID_KEYSTORE_PASSWORD',
                        '--key-pass', 'env:SPECIAL_ANDROID_KEYSTORE_PASSWORD',
                        '--v1-signing-enabled', 'false', '--v2-signing-enabled', 'true',
                        '--v3-signing-enabled', 'true', '--v4-signing-enabled', 'false',
                        '--out', str(apk), str(unsigned)], capture_output=True, check=True)
        SIGNING.verify([apk])
        fingerprint_file = self.root / 'dist/SIGNING_CERTIFICATE_SHA256'
        self.assertEqual(fingerprint_file.read_text(), self.fingerprint + '\n')
        fingerprint_file.unlink()
        shutil.copyfile(ROOT / 'android/special-signing-certificate.pem',
                        self.root / 'android/special-signing-certificate.pem')
        with self.assertRaisesRegex(ValueError, 'fixed release certificate'):
            SIGNING.verify([apk])
        self.assertFalse(fingerprint_file.exists())

    def test_sdk_range_signers_can_repeat_the_same_fixed_certificate(self):
        for dev_release in ('', ' (dev release=true)'):
            with self.subTest(dev_release=dev_release):
                self.verify('\n'.join([
                    f'Signer (minSdkVersion=33{dev_release}, maxSdkVersion=2147483647) '
                    f'certificate SHA-256 digest: {self.fingerprint.upper()}',
                    'Signer (minSdkVersion=24, maxSdkVersion=32) '
                    f'certificate SHA-256 digest: {self.fingerprint}',
                    f'Source Stamp Signer certificate SHA-256 digest: {"0" * 64}',
                ]))
                self.assertEqual((self.root / 'dist/SIGNING_CERTIFICATE_SHA256').read_text(),
                                 self.fingerprint + '\n')

    def test_a_different_sdk_range_signer_stops_publication(self):
        output = '\n'.join([
            'Signer (minSdkVersion=33, maxSdkVersion=2147483647) '
            f'certificate SHA-256 digest: {self.fingerprint}',
            f'Signer (minSdkVersion=24, maxSdkVersion=32) certificate SHA-256 digest: {"0" * 64}',
        ])
        with self.assertRaisesRegex(ValueError, 'fixed release certificate'):
            self.verify(output)
        self.assertFalse((self.root / 'dist/SIGNING_CERTIFICATE_SHA256').exists())

    def test_missing_signer_fingerprints_stop_publication(self):
        for output in ('Verifies', f'Source Stamp Signer certificate SHA-256 digest: {self.fingerprint}'):
            with self.subTest(output=output):
                with self.assertRaisesRegex(ValueError, 'No APK signing certificate'):
                    self.verify(output)
                self.assertFalse((self.root / 'dist/SIGNING_CERTIFICATE_SHA256').exists())

    def test_different_or_invalid_apk_signatures_stop_publication(self):
        for fingerprint, status in (('0' * 64, 0), (self.fingerprint, 1)):
            with self.subTest(fingerprint=fingerprint, status=status):
                with self.assertRaises(ValueError):
                    self.verify(f'Signer #1 certificate SHA-256 digest: {fingerprint}', status)
                self.assertFalse((self.root / 'dist/SIGNING_CERTIFICATE_SHA256').exists())


if __name__ == '__main__':
    unittest.main()
