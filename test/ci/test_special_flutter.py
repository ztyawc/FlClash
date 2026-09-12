import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class SpecialFlutterTest(unittest.TestCase):
    def test_native_hooks_are_restored_on_success_and_failure(self):
        script = Path(__file__).resolve().parents[2] / 'tool/test_special_flutter.sh'
        for exit_code in (0, 1):
            with self.subTest(exit_code=exit_code), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                (root / 'tool').mkdir()
                shutil.copyfile(script, root / 'tool' / script.name)
                original = ('hooks:\n  user_defines:\n    setup:\n'
                            '      build_assets: true\n    rust_api:\n'
                            '      build_assets: true\n')
                config = root / 'pubspec.yaml'
                config.write_text(original)
                flutter = root / 'flutter'
                flutter.write_text('#!/usr/bin/env bash\n'
                                   'test "$(grep -c "build_assets: false" pubspec.yaml)" -eq 2 || exit 99\n'
                                   'exit "$TEST_EXIT_CODE"\n')
                flutter.chmod(0o755)
                env = {**os.environ, 'PATH': f'{root}:{os.environ["PATH"]}',
                       'TEST_EXIT_CODE': str(exit_code)}
                result = subprocess.run(['bash', str(root / 'tool' / script.name)], env=env)
                self.assertEqual(result.returncode, exit_code)
                self.assertEqual(config.read_text(), original)


if __name__ == '__main__':
    unittest.main()
