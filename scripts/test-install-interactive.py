#!/usr/bin/env python3
"""Drive only the installer's known prompts in a disposable PTY for native tests."""
import os
import pty
import select
import signal
import sys
import time

command = sys.argv[1:]
if not command:
    raise SystemExit('An installer command is required')
pid, terminal = pty.fork()
if pid == 0:
    os.execvp(command[0], command)
pending = b''
status = None
try:
    deadline = time.monotonic() + 600
    while time.monotonic() < deadline:
        if select.select([terminal], [], [], .2)[0]:
            try:
                chunk = os.read(terminal, 65536)
            except OSError:
                break
            if not chunk:
                break
            sys.stdout.buffer.write(chunk); sys.stdout.buffer.flush()
            pending += chunk
            for prompt, answer in ((b'Administrator username [owner]: ', b'owner\n'),
                                   (b'Administrator password (at least 12 bytes): ', b'interactive-fixture-password-2026\n'),
                                   (b'Confirm password: ', b'interactive-fixture-password-2026\n'),
                                   (b'Runtime mode: [r] server / [s] sandbox node [r]: ', b'r\n'),
                                   (b'Resource preset: [p] permissive / [l] least privilege [l]: ', b'p\n'),
                                   (b'Fix permissions [f, recommended], ignore and continue [i], or quit [q]? ', b'f\n'),
                                   (b'Apply this installation plan? [y/N] ', b'y\n'),
                                   (b'Install system dependencies with APT? [y/N] ', b'y\n'),
                                   (b'Prepare and load the distribution Bubblewrap AppArmor policy (preserve existing policies)? [y/N] ', b'y\n')):
                if prompt in pending:
                    pending = pending.split(prompt, 1)[1]
                    os.write(terminal, answer)
            pending = pending[-4096:]
        waited, result = os.waitpid(pid, os.WNOHANG)
        if waited:
            status = result
            # Drain the remaining PTY output after process exit.
            while select.select([terminal], [], [], 0)[0]:
                try: chunk = os.read(terminal, 65536)
                except OSError: break
                if not chunk: break
                sys.stdout.buffer.write(chunk)
            break
    else:
        raise TimeoutError('Interactive installer did not complete within ten minutes')
finally:
    if status is None:
        waited, result = os.waitpid(pid, os.WNOHANG)
        if waited:
            status = result
        else:
            os.kill(pid, signal.SIGTERM)
            _, status = os.waitpid(pid, 0)
    os.close(terminal)
sys.exit(os.waitstatus_to_exitcode(status))
