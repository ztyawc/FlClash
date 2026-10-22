import base64
import hashlib
import importlib.util
import os
from pathlib import Path
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

    def verify(self, fingerprint, status=0):
        signer = self.root / 'sdk/build-tools/36.0.0/apksigner'
        signer.parent.mkdir(parents=True, exist_ok=True)
        signer.write_text(f'#!/bin/sh\nprintf "%s\\n" '
                          f'"Signer #1 certificate SHA-256 digest: {fingerprint}"\nexit {status}\n')
        signer.chmod(0o755)
        apks = [self.root / 'dist' / name for name in ('arm64.apk', 'arm32.apk')]
        for apk in apks:
            apk.touch()
        with mock.patch.dict(os.environ, {'ANDROID_HOME': str(self.root / 'sdk')}):
            SIGNING.verify(apks)

    def test_verified_apks_record_the_fixed_fingerprint(self):
        self.verify(self.fingerprint)
        self.assertEqual((self.root / 'dist/SIGNING_CERTIFICATE_SHA256').read_text(),
                         self.fingerprint + '\n')

    def test_different_or_invalid_apk_signatures_stop_publication(self):
        for fingerprint, status in (('0' * 64, 0), (self.fingerprint, 1)):
            with self.subTest(fingerprint=fingerprint, status=status):
                with self.assertRaises(ValueError):
                    self.verify(fingerprint, status)
                self.assertFalse((self.root / 'dist/SIGNING_CERTIFICATE_SHA256').exists())


if __name__ == '__main__':
    unittest.main()
