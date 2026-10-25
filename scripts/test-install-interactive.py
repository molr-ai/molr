#!/usr/bin/env python3
"""Drive only the installer's known prompts in a disposable PTY for native tests."""
import os
import pty
import select
import signal
import sys
import time


def run(command, timeout=600, terminate_timeout=2):
    """Exercise known prompts; terminate the isolated PTY group on completion."""
    pid, terminal = pty.fork()
    if pid == 0:
        os.execvp(command[0], command)
    pending = b''
    status = None
    try:
        deadline = time.monotonic() + timeout
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
                                       (b'Resource preset: [p] personal / [l] least privilege [p]: ', b'\n'),
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
            raise TimeoutError(f'Interactive installer did not complete within {timeout:g} seconds')
    finally:
        # molr-setup handles SIGTERM via context cancellation, but a terminal read
        # can remain blocked. Never wait indefinitely for cooperative shutdown.
        try:
            os.killpg(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        if status is None:
            deadline = time.monotonic() + terminate_timeout
            while time.monotonic() < deadline:
                waited, result = os.waitpid(pid, os.WNOHANG)
                if waited:
                    status = result
                    break
                time.sleep(.05)
        # Reclaim remaining children in the PTY group even if its leader exited.
        try:
            os.killpg(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        if status is None:
            _, status = os.waitpid(pid, 0)
        os.close(terminal)
    return os.waitstatus_to_exitcode(status)


if __name__ == '__main__':
    if not sys.argv[1:]:
        raise SystemExit('An installer command is required')
    sys.exit(run(sys.argv[1:]))
