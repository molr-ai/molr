"""Rebuild the committed codec: python3 build.py <wasi-sdk-root> <silk-source-root>.

WASI SDK 34.0 and kn007/silk-v3-decoder commit
507be6bca8ce1fb977a061481f1d79e8c610e309 are pinned in README.md.
No source download or package scripts are performed by this build.
"""
import pathlib
import subprocess
import sys

here = pathlib.Path(__file__).resolve().parent
wasi, sdk = (pathlib.Path(value).resolve() for value in sys.argv[1:])
subprocess.run([
    str(wasi / 'bin/clang'), '-O2', '-DNDEBUG', '-fwrapv', '-fno-strict-aliasing',
    '-mexec-model=reactor', '-I' + str(sdk / 'silk/interface'),
    '-I' + str(sdk / 'silk/src'), str(here / 'decode.c'),
    *[str(p) for p in sorted((sdk / 'silk/src').glob('*.c'))],
    '-Wl,--export=decode_silk', '-Wl,--export=malloc', '-Wl,--strip-all',
    '-Wl,--initial-memory=8388608', '-Wl,--max-memory=33554432',
    '-o', str(here.parent / 'silk.wasm'),
], check=True)

(here.parent / 'silk.wasm').chmod(0o644)
