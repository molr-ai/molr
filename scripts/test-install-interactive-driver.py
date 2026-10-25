#!/usr/bin/env python3
"""Fast PTY regressions; fixtures never install software or start services."""
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parent.parent
DRIVER = ROOT / 'scripts/test-install-interactive.py'


class InteractiveDriverTests(unittest.TestCase):
    def invoke(self, source, timeout=3, terminate_timeout=.2):
        # Import in a separate process so PTY output and exit status follow the
        # actual command-line path, while deadline tests finish quickly.
        launcher = '''
import importlib.util, sys
spec = importlib.util.spec_from_file_location('driver', sys.argv[1])
driver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(driver)
sys.exit(driver.run([sys.executable, '-c', sys.argv[2]], timeout=float(sys.argv[3]), terminate_timeout=float(sys.argv[4])))
'''
        return subprocess.run([sys.executable, '-c', launcher, str(DRIVER), source,
                               str(timeout), str(terminate_timeout)],
                              capture_output=True, text=True, timeout=8)

    def test_current_installer_prompt_accepts_personal_default_across_chunks(self):
        source = (ROOT / 'runtime/internal/installation/setup.go').read_text()
        # Read the production prompt so changing its wording breaks this test
        # unless the PTY driver is updated in the same change.
        prompt = re.search(r'e\.UI\.answer\("(Resource preset: [^"]+)"\)', source).group(1)
        result = self.invoke(f'''
import sys, time
prompt = {prompt!r}
print('Installation fixture', flush=True)
sys.stdout.write(prompt[:20]); sys.stdout.flush(); time.sleep(.05)
sys.stdout.write(prompt[20:]); sys.stdout.flush()
assert input() == '', 'Accept the installer default with Enter'
print('PERSONAL_DEFAULT_ACCEPTED', flush=True)
''')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('PERSONAL_DEFAULT_ACCEPTED', result.stdout)

    def test_known_confirmation_is_answered_and_output_drained(self):
        result = self.invoke("print('Apply this installation plan? [y/N] ', end='', flush=True); assert input() == 'y'; print('CONFIRMED', flush=True)")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('CONFIRMED', result.stdout)

    def test_installer_failure_is_preserved(self):
        result = self.invoke("import sys; print('fixture failure', flush=True); sys.exit(7)")
        self.assertEqual(result.returncode, 7)
        self.assertIn('fixture failure', result.stdout)

    def test_unknown_prompt_times_out_and_reaps_child_ignoring_sigterm(self):
        with tempfile.TemporaryDirectory() as directory:
            pid_path = Path(directory) / 'child.pid'
            source = f'''
import os, signal
from pathlib import Path
signal.signal(signal.SIGTERM, signal.SIG_IGN)
Path({str(pid_path)!r}).write_text(str(os.getpid()))
print('Unknown installer question: ', end='', flush=True)
input()
print('UNEXPECTED_ANSWER', flush=True)
'''
            started = time.monotonic()
            try:
                result = self.invoke(source, timeout=.5, terminate_timeout=.1)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('Interactive installer did not complete', result.stderr)
                self.assertNotIn('UNEXPECTED_ANSWER', result.stdout)
                self.assertLess(time.monotonic() - started, 5)
                pid = int(pid_path.read_text())
                with self.assertRaises(ProcessLookupError):
                    os.kill(pid, 0)
            finally:
                # Bound cleanup even if the driver regresses and the outer
                # subprocess timeout fires; the fixture owns this PTY group.
                if pid_path.exists():
                    try:
                        os.killpg(int(pid_path.read_text()), signal.SIGKILL)
                    except ProcessLookupError:
                        pass


if __name__ == '__main__':
    unittest.main()
