#!/usr/bin/env python3
"""Exercise bootstrap diff review with piped script input and a real terminal."""
import errno
import os
from pathlib import Path
import select
import shutil
import signal
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
SOURCE = (ROOT / 'release/bootstrap-template.sh').read_text()
FUNCTIONS = SOURCE[SOURCE.index('bootstrap_git() {'):SOURCE.index('validate_checkout_identity() {')]


class RecoveryReview(unittest.TestCase):
    def review(self, viewer, action='c', tty=True):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            repo = base / 'checkout with spaces'
            repo.mkdir()
            env = dict(os.environ, HOME=str(base), GIT_CONFIG_GLOBAL='/dev/null',
                       GIT_CONFIG_NOSYSTEM='1', TERM='xterm-256color')
            env.pop('USERLAND_NO_TTY', None)
            def git(*args):
                return subprocess.check_output(['git', '-C', str(repo), *args], env=env)
            git('init', '-q')
            git('config', 'user.name', 'Test')
            git('config', 'user.email', 'test@example.invalid')
            config = repo / 'config.toml'
            config.write_text('value = "original"\n')
            git('add', '.')
            git('-c', 'commit.gpgsign=false', 'commit', '-qm', 'initial')
            config.write_text('value = "staged"\n')
            git('add', '.')
            config.write_text('value = "working"\n')
            (repo / 'untracked').write_text('untracked content must not be displayed\n')
            bin_dir = base / 'bin'
            bin_dir.mkdir()
            for command in ['git', 'cat', 'mktemp', 'rm']:
                (bin_dir / command).symlink_to(shutil.which(command))
            if viewer != 'none':
                name = 'less' if viewer == 'less' else 'delta'
                executable = bin_dir / name
                executable.write_text('''#!/bin/sh
[ -t 1 ] || exit 10
printf 'VIEWER:%s\\n' "$*"
cat
printf 'VIEWER_WAIT\\n'
read -r answer </dev/tty
[ "$answer" = q ] || exit 11
''' if viewer != 'broken' else '#!/bin/sh\nexit 1\n')
                executable.chmod(0o755)
            env['PATH'] = str(bin_dir)
            script = ('set -eu\ntag=v0.0.0\ndie() { exit 1; }\n' + FUNCTIONS +
                      '\nrecover_checkout_changes "$1"\n')
            if not tty:
                result = subprocess.run(['/bin/sh', '-s', '--', str(repo)], input=script.encode(),
                                        env=env, capture_output=True, start_new_session=True, timeout=5)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(b'will not overwrite', result.stderr)
                self.assertIn(b'MM config.toml', git('status', '--porcelain'))
                return
            read_fd, write_fd = os.pipe()
            pid, master = os.forkpty()
            if pid == 0:
                os.close(write_fd)
                os.dup2(read_fd, 0)
                os.close(read_fd)
                os.execve('/bin/sh', ['sh', '-s', '--', str(repo)], env)
            os.close(read_fd)
            os.write(write_fd, script.encode())
            os.close(write_fd)
            output = b''
            answered_viewer = answered_action = False
            try:
                deadline = time.monotonic() + 8
                while True:
                    if time.monotonic() > deadline:
                        self.fail('recovery did not finish: ' + repr(output))
                    if select.select([master], [], [], .05)[0]:
                        try:
                            chunk = os.read(master, 65536)
                        except OSError as exc:
                            if exc.errno != errno.EIO:
                                raise
                            break
                        if not chunk:
                            break
                        output += chunk
                    if b'VIEWER_WAIT' in output and not answered_viewer:
                        os.write(master, b'q\n')
                        answered_viewer = True
                    if b'[s] Stash and continue' in output and not answered_action:
                        os.write(master, (action + '\n').encode())
                        answered_action = True
                _, status = os.waitpid(pid, 0)
                pid = None
                self.assertTrue(answered_action, output)
                self.assertEqual(os.waitstatus_to_exitcode(status), 0 if action == 's' else 1, output)
                self.assertIn(b'+value = "staged"', output)
                self.assertIn(b'+value = "working"', output)
                self.assertNotIn(b'untracked content must not be displayed', output)
                if viewer == 'delta':
                    self.assertIn(b'--paging always --pager less -R -X', output)
                self.assertNotIn(b'MM config.toml', output)
                if action == 's':
                    self.assertEqual(git('status', '--porcelain'), b'')
                    self.assertIn(b'userland before', git('stash', 'list'))
                else:
                    self.assertEqual(config.read_text(), 'value = "working"\n')
            finally:
                if pid is not None:
                    os.kill(pid, signal.SIGKILL)
                    os.waitpid(pid, 0)
                os.close(master)

    def test_delta_then_stash(self):
        self.review('delta', 's')

    def test_less_then_cancel(self):
        self.review('less')

    def test_plain_fallback(self):
        self.review('none')

    def test_broken_delta_fallback(self):
        self.review('broken')

    def test_no_terminal_preserves_changes(self):
        self.review('none', tty=False)


if __name__ == '__main__':
    unittest.main()
