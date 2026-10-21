"""Exercise the actual workflow merge blocks against disposable Git repositories."""
import pathlib
import subprocess
import tempfile
import textwrap
import unittest


WORKFLOW = pathlib.Path(__file__).resolve().parents[2] / '.github/workflows/sync-special.yml'
CORE_MODULE = '[submodule "core/Clash.Meta"]\n\tpath = core/Clash.Meta\n\turl = {}\n'


class SyncMergeTest(unittest.TestCase):
    def check_merge(self, source_conflict=False, missing_source=False,
                    core_conflict=False, dependency_conflict=False,
                    upstream_core_only=False):
        workflow = WORKFLOW.read_text()
        blocks = workflow.split('            previous_head=')[1:]
        self.assertEqual(len(blocks), 2)
        for index, block in enumerate(blocks):
            script = textwrap.dedent('            previous_head=' + block.split('            git commit --no-edit')[0])
            if index == 0:
                script = 'source_ref=source/main\n' + script
            with tempfile.TemporaryDirectory(prefix='flclash-merge-test-') as folder:
                root = pathlib.Path(folder)

                def git(*args):
                    return subprocess.run(['git', *args], cwd=root, check=True, capture_output=True, text=True)

                def write(name, value):
                    file = root / name
                    file.parent.mkdir(parents=True, exist_ok=True)
                    file.write_text(value)

                def set_core(revision):
                    git('update-index', '--add', '--cacheinfo',
                        f'160000,{revision},core/Clash.Meta')

                git('init', '-b', 'main')
                git('config', 'user.name', 'CI test')
                git('config', 'user.email', 'test@example.invalid')
                owned = ['build-special-android.yml', 'sync-special.yml']
                for name in owned:
                    write(f'.github/workflows/{name}', 'base\n')
                write('.github/workflows/build.yaml', 'base\n')
                write('source.txt', 'base\n')
                if index == 1:
                    write('.gitmodules', CORE_MODULE.format('https://example.invalid/base.git'))
                    write('core/go.mod', 'base dependencies\n')
                    write('core/go.sum', 'base checksums\n')
                git('add', '.')
                if index == 1:
                    set_core('1' * 40)
                git('commit', '-m', 'base')
                git('switch', '-c', 'source/main')
                for name in owned:
                    write(f'.github/workflows/{name}', 'upstream\n')
                write('.github/workflows/build.yaml', 'upstream\n')
                write('.github/workflows/upstream-only.yml', 'new workflow\n')
                write('upstream.txt', 'new upstream feature\n')
                if source_conflict:
                    write('source.txt', 'upstream\n')
                if index == 1 and (core_conflict or upstream_core_only):
                    write('.gitmodules', CORE_MODULE.format('https://example.invalid/upstream.git'))
                if index == 1 and dependency_conflict:
                    write('core/go.mod', 'new upstream dependencies and tools\n')
                    write('core/go.sum', 'new upstream checksums\n')
                git('add', '.')
                if index == 1:
                    set_core(('2' if core_conflict or upstream_core_only else '1') * 40)
                git('commit', '-m', 'upstream')
                git('switch', 'main')
                for name in owned:
                    write(f'.github/workflows/{name}', 'special edition\n')
                if source_conflict:
                    write('source.txt', 'special edition\n')
                if index == 1:
                    write('.gitmodules', CORE_MODULE.format('https://example.invalid/special.git'))
                    if dependency_conflict:
                        write('core/go.mod', 'special core dependencies\n')
                        write('core/go.sum', 'special core checksums\n')
                git('add', '.')
                if index == 1:
                    set_core(('1' if upstream_core_only else '3') * 40)
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
                    expected = 'base\n' if index == 0 else 'upstream\n'
                    self.assertEqual((root / '.github/workflows/build.yaml').read_text(), expected)
                    self.assertEqual((root / '.github/workflows/upstream-only.yml').exists(), index == 1)
                    if index == 1:
                        self.assertEqual((root / '.gitmodules').read_text(),
                                         CORE_MODULE.format('https://example.invalid/special.git'))
                        revision = ('1' if upstream_core_only else '3') * 40
                        self.assertIn(revision, git('ls-files', '--stage', 'core/Clash.Meta').stdout)
                        if dependency_conflict:
                            self.assertEqual((root / 'core/go.mod').read_text(),
                                             'new upstream dependencies and tools\n')
                            self.assertEqual((root / 'core/go.sum').read_text(),
                                             'new upstream checksums\n')
                for name in owned:
                    self.assertEqual((root / f'.github/workflows/{name}').read_text(), 'special edition\n')
                if source_conflict:
                    self.assertEqual(git('diff', '--name-only', '--diff-filter=U').stdout, 'source.txt\n')

    def test_workflow_conflicts_keep_our_workflows(self):
        self.check_merge()

    def test_source_conflicts_stop_sync(self):
        self.check_merge(source_conflict=True)

    def test_non_conflict_merge_errors_stop_sync(self):
        self.check_merge(missing_source=True)

    def test_core_and_dependency_conflicts_keep_the_maintained_core(self):
        self.check_merge(core_conflict=True, dependency_conflict=True)

    def test_nonconflicting_upstream_core_changes_keep_the_maintained_core(self):
        self.check_merge(upstream_core_only=True)

    def test_source_conflicts_still_stop_after_core_conflicts_are_resolved(self):
        self.check_merge(source_conflict=True, core_conflict=True,
                         dependency_conflict=True)


if __name__ == '__main__':
    unittest.main()
