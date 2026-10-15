"""Write the same reproducible release archive on BSD and GNU hosts."""
import gzip
import pathlib
import sys
import tarfile

stage, output, epoch, *members = sys.argv[1:]
with open(output, 'wb') as raw:
    with gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode='w', format=tarfile.USTAR_FORMAT) as archive:
            for name in sorted(members):
                source = pathlib.Path(stage) / name
                if source.is_symlink() or not source.is_file():
                    raise ValueError(f'Regular package file required: {name}')
                info = archive.gettarinfo(str(source), arcname='molr/' + name)
                info.uid = info.gid = 0
                info.uname = info.gname = ''
                info.mtime = int(epoch)
                info.mode = 0o755 if name.startswith('bin/') else 0o644
                with source.open('rb') as content:
                    archive.addfile(info, content)
