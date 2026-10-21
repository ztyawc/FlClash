import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest
import zipfile


ROOT = Path(__file__).resolve().parents[2]


def workflow_script(workflow, name):
    content = (ROOT / '.github/workflows' / workflow).read_text()
    step = content.split(f'      - name: {name}\n', 1)[1].split('\n      - name:', 1)[0]
    return textwrap.dedent(step.split('        run: |\n', 1)[1])


class SpecialAndroidTest(unittest.TestCase):
    def verify_apks(self, legacy, missing_library=None):
        script = workflow_script('build-special-android.yml', 'Verify and checksum APKs')
        with tempfile.TemporaryDirectory(prefix='flclash-apk-test-') as folder:
            root = Path(folder)
            if legacy:
                source = root / 'android/core/src/main/cpp/core.cpp'
                source.parent.mkdir(parents=True)
                source.touch()
            sdk = root / 'sdk'
            aapt = sdk / 'build-tools/1/aapt'
            aapt.parent.mkdir(parents=True)
            aapt.write_text('#!/bin/sh\n'
                            'printf "%s\\n" "package: name=\'com.follow.clash.dev\'" '
                            '"application-label:\'FlClash（特供版）\'"\n')
            aapt.chmod(0o755)
            dist = root / 'dist'
            dist.mkdir()
            libraries = ['libclash.so', 'librust_api.so']
            if legacy:
                libraries.append('libcore.so')
            for abi in ('arm64-v8a', 'armeabi-v7a', 'x86_64'):
                with zipfile.ZipFile(dist / f'FlClash-{abi}.apk', 'w') as apk:
                    for library in libraries:
                        if library != missing_library:
                            apk.writestr(f'lib/{abi}/{library}', b'native library fixture')
            result = subprocess.run(['bash', '-euo', 'pipefail', '-c', script], cwd=root,
                                    env={**os.environ, 'ANDROID_HOME': str(sdk)},
                                    capture_output=True, text=True)
            if missing_library:
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((dist / 'SHA256SUMS').exists())
            else:
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(len((dist / 'SHA256SUMS').read_text().splitlines()), 3)

    def test_legacy_and_gomobile_apks_are_verified(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                self.verify_apks(legacy)

    def test_missing_native_libraries_fail_verification(self):
        for legacy, library in ((True, 'libcore.so'), (False, 'libclash.so'),
                                (False, 'librust_api.so')):
            with self.subTest(legacy=legacy, library=library):
                self.verify_apks(legacy, missing_library=library)

    def test_rust_setup_follows_the_checked_out_toolchain(self):
        for workflow in ('build-special-android.yml', 'sync-special.yml'):
            for version in ('1.95.0', '1.99.0'):
                with self.subTest(workflow=workflow, version=version), tempfile.TemporaryDirectory() as folder:
                    root = Path(folder)
                    manifest = root / 'plugins/rust_api/rust/rust-toolchain.toml'
                    manifest.parent.mkdir(parents=True)
                    manifest.write_text(f'[toolchain]\nchannel = "{version}"\n')
                    rustup = root / 'rustup'
                    rustup.write_text('#!/bin/sh\nprintf "%s\\n" "$@"\n')
                    rustup.chmod(0o755)
                    script = workflow_script(workflow, 'Set up Rust for Android')
                    result = subprocess.run(['bash', '-euo', 'pipefail', '-c', script], cwd=root,
                                            env={**os.environ, 'PATH': f'{root}:{os.environ["PATH"]}'},
                                            check=True, capture_output=True, text=True)
                    self.assertEqual(result.stdout.splitlines()[2], version)


if __name__ == '__main__':
    unittest.main()
