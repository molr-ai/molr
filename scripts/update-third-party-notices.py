#!/usr/bin/env python3
"""Refresh notices from installed, locked dependencies; preserve the reviewed supplement.

Run after npm ci and with runtime/.go-version available. No third-party code is run.
The supplement in THIRD_PARTY_NOTICES.md covers embedded sources/fonts and missing
upstream notice files; updating it requires reviewing the relevant source version.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parent.parent
BEGIN = '<!-- BEGIN GENERATED NOTICES -->'
END = '<!-- END GENERATED NOTICES -->'
NOTICE = re.compile(r'^(?:licen[sc]e|copying|notice|copyright)(?:$|[.-])', re.I)


def stream_json(text):
    decoder = json.JSONDecoder()
    offset = 0
    while offset < len(text):
        if text[offset].isspace():
            offset += 1
            continue
        value, offset = decoder.raw_decode(text, offset)
        yield value


def documents(directory):
    return [p for p in sorted(directory.iterdir()) if p.is_file() and NOTICE.match(p.name)
            and p.name != 'LICENSE-LOGO']


def block(label, text):
    # Keep complete original texts, including notices containing Markdown fences.
    longest = max((len(m.group()) for m in re.finditer(r'`+', text)), default=0)
    fence = '`' * max(4, longest + 1)
    return f'\n### {label}\n\n{fence}text\n{text.rstrip()}\n{fence}\n'


def generate():
    parts = ['\n## Dependency license texts\n\n'
             'This is a superset for Molr frontend distributions and Linux/macOS Runtime builds; '
             'not every component is present in every artifact. Original licenses apply '
             'only to their respective components. Vite/Rolldown notices are included for generated runtime helpers; the build tools themselves are not shipped.\n']
    lock = json.loads((ROOT / 'package-lock.json').read_text())
    for location, package in sorted(lock['packages'].items()):
        if 'node_modules/' not in location or package.get('link') or (package.get('dev') and location not in ('node_modules/vite', 'node_modules/rolldown')):
            continue
        directory = ROOT / location
        manifest = json.loads((directory / 'package.json').read_text())
        if manifest['version'] != package['version']:
            raise RuntimeError(f'Installed version differs from lockfile: {location}; run npm ci')
        name = manifest['name']
        version = manifest['version']
        texts = documents(directory)
        parts.append(f'\n## npm: {name} {version}\n\n')
        parts.append(f'Package: https://www.npmjs.com/package/{name}/v/{version}\n\n')
        if name == 'dompurify':
            parts.append('Molr elects the Apache-2.0 option of the upstream dual license.\n')
        if not texts:
            if name not in ('remark-math', 'rehype-katex'):
                raise RuntimeError(f'No license text found: {location}')
            parts.append('MIT; full original copyright and license are in the remark-math supplement below.\n')
        for path in texts:
            parts.append(block(path.name, path.read_text()))
    go = os.environ.get('GO', 'go')
    env = dict(os.environ, GOTOOLCHAIN='go' + (ROOT / 'runtime/.go-version').read_text().strip(),
               CGO_ENABLED='0', GOOS='linux')
    env.setdefault('GOMODCACHE', str(ROOT / '.cache/go-mod'))
    env.setdefault('GOCACHE', str(ROOT / '.cache/go-build'))
    def run(*args, **extra):
        return subprocess.check_output([go, *args], cwd=ROOT / 'runtime', env={**env, **extra}, text=True)
    modules = list(stream_json(run('mod', 'download', '-json')))
    module_dirs = {}
    for module in sorted(modules, key=lambda m: m['Path']):
        if module.get('Error'):
            raise RuntimeError(module['Error'])
        directory = Path(module['Dir'])
        module_dirs[module['Path']] = directory
        parts.append(f"\n## Go module: {module['Path']} {module['Version']}\n")
        parts.append(f"\nSource: https://pkg.go.dev/{module['Path']}@{module['Version']}\n")
        for path in documents(directory):
            parts.append(block(path.name, path.read_text()))
    goroot = Path(run('env', 'GOROOT').strip())
    version = (ROOT / 'runtime/.go-version').read_text().strip()
    parts.append(f'\n## Go toolchain runtime and standard library {version}\n')
    parts.append(f'\nSource: https://go.dev/dl/#go{version}\n')
    for path in documents(goroot):
        parts.append(block(path.name, path.read_text()))
    # Include more-specific notices above packages compiled on each supported Runtime OS and CPU.
    specific = set()
    for goos, arch in (('linux', 'amd64'), ('linux', 'arm64'), ('darwin', 'amd64'), ('darwin', 'arm64')):
        for package in stream_json(run('list', '-deps', '-json', './cmd/molr', './cmd/molr-setup', GOOS=goos, GOARCH=arch)):
            directory = Path(package['Dir'])
            module = package.get('Module')
            if module and module.get('Main'):
                continue
            boundary = module_dirs[module['Path']] if module else goroot
            while directory != boundary:
                if not directory.is_relative_to(boundary):
                    raise RuntimeError(f'Unexpected dependency path: {directory}')
                for path in documents(directory):
                    specific.add((str(path.relative_to(boundary)), str(boundary), path.read_text()))
                directory = directory.parent
    for relative, boundary, text in sorted(specific):
        owner = 'Go standard library' if Path(boundary) == goroot else Path(boundary).name
        parts.append(block(f'{owner}: {relative}', text))
    return ''.join(parts)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='fail if generated dependency notices differ')
    args = parser.parse_args()
    path = ROOT / 'THIRD_PARTY_NOTICES.md'
    current = path.read_text()
    before, remainder = current.split(BEGIN, 1)
    _, after = remainder.split(END, 1)
    updated = before + BEGIN + '\n' + generate() + '\n' + END + after
    if args.check:
        if current != updated:
            raise SystemExit('Third-party notices are stale; review dependencies and run npm run notices:update.')
        print('Third-party notice texts match installed locked dependencies (Linux/macOS, amd64/arm64).')
    else:
        path.write_text(updated)
        print(f'Updated {path.name} ({len(updated.encode())} bytes).')


if __name__ == '__main__':
    main()
