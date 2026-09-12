"""Exercise the actual workflow merge blocks against disposable Git repositories."""
import pathlib
import subprocess
import tempfile
import textwrap
import unittest


WORKFLOW = pathlib.Path(__file__).resolve().parents[2] / '.github/workflows/sync-special.yml'


class SyncMergeTest(unittest.TestCase):
    def check_merge(self, source_conflict=False, missing_source=False):
        workflow = WORKFLOW.read_text()
        blocks = workflow.split('            previous_head=')[1:]
        self.assertEqual(len(blocks), 2)
        for block in blocks:
            script = textwrap.dedent('            previous_head=' + block.split('            git commit --no-edit')[0])
            with tempfile.TemporaryDirectory(prefix='flclash-merge-test-') as folder:
                root = pathlib.Path(folder)

                def git(*args):
                    return subprocess.run(['git', *args], cwd=root, check=True, capture_output=True, text=True)

                def write(name, value):
                    file = root / name
                    file.parent.mkdir(parents=True, exist_ok=True)
                    file.write_text(value)

                git('init', '-b', 'main')
                git('config', 'user.name', 'CI test')
                git('config', 'user.email', 'test@example.invalid')
                write('.github/workflows/test.yml', 'base\n')
                write('source.txt', 'base\n')
                git('add', '.')
                git('commit', '-m', 'base')
                git('switch', '-c', 'source/main')
                write('.github/workflows/test.yml', 'upstream\n')
                write('upstream.txt', 'new upstream feature\n')
                if source_conflict:
                    write('source.txt', 'upstream\n')
                git('add', '.')
                git('commit', '-m', 'upstream')
                git('switch', 'main')
                write('.github/workflows/test.yml', 'special edition\n')
                if source_conflict:
                    write('source.txt', 'special edition\n')
                git('add', '.')
                git('commit', '-m', 'special edition')
                if missing_source:
                    git('branch', '-D', 'source/main')
                result = subprocess.run(['bash', '-euo', 'pipefail', '-c', script], cwd=root, capture_output=True, text=True)
                if source_conflict or missing_source:
                    self.assertNotEqual(result.returncode, 0)
                else:
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertEqual(git('diff', '--name-only', '--diff-filter=U').stdout, '')
                    self.assertEqual((root / 'upstream.txt').read_text(), 'new upstream feature\n')
                self.assertEqual((root / '.github/workflows/test.yml').read_text(), 'special edition\n')
                if source_conflict:
                    self.assertIn('source.txt', git('diff', '--name-only', '--diff-filter=U').stdout)

    def test_workflow_conflicts_keep_our_workflows(self):
        self.check_merge()

    def test_source_conflicts_stop_sync(self):
        self.check_merge(source_conflict=True)

    def test_non_conflict_merge_errors_stop_sync(self):
        self.check_merge(missing_source=True)


if __name__ == '__main__':
    unittest.main()
