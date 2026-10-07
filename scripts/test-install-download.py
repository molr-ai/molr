#!/usr/bin/env python3
"""Download bootstrap rejects damaged packages before executing an installer."""
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[1]
class BootstrapTests(unittest.TestCase):
    def test_checksum_rejected_before_extract(self):
        with tempfile.TemporaryDirectory() as d:
            archive=Path(d)/'bad.tar.gz'
            archive.write_bytes(b'not an archive')
            Path(str(archive)+'.sha256').write_text('0'*64+'  bad.tar.gz\n')
            result=subprocess.run(['bash',str(ROOT/'scripts/install.sh'),'--archive',str(archive)],capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0)
            self.assertIn('Checksum mismatch',result.stderr)
    def test_verified_archive_cannot_escape_destination(self):
        with tempfile.TemporaryDirectory() as d:
            archive=Path(d)/'bad.tar.gz'
            with tarfile.open(archive,'w:gz') as t:
                content=b'escaped';item=tarfile.TarInfo('../escaped');item.size=len(content);t.addfile(item,io.BytesIO(content))
            Path(str(archive)+'.sha256').write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  bad.tar.gz\n')
            result=subprocess.run(['bash',str(ROOT/'scripts/install.sh'),'--archive',str(archive)],capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0)
            self.assertIn('Unsafe archive member',result.stderr)
if __name__=='__main__': unittest.main()
