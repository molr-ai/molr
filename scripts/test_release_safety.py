#!/usr/bin/env python3
"""Use synthetic data and disposable repositories; never load real credentials."""
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

import release_safety as safety


class ReleaseSafetyTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='molr-release-safety-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def repo(self):
        safety.git('init', '-q', root=self.root)
        (self.root/'.gitignore').write_bytes((safety.ROOT/'.gitignore').read_bytes())
        (self.root/'README.md').write_text('Committed public source\n')
        (self.root/'.env.example').write_text('MODE=example\n')
        safety.git('add', '.', root=self.root)
        self.commit()

    def commit(self):
        safety.git('-c', 'user.name=Release test', '-c', 'user.email=release@example.invalid',
                   '-c', 'core.hooksPath=/dev/null', 'commit', '--no-gpg-sign', '-qm', 'fixture', root=self.root)

    def credential(self):
        return ('ghp' + '_' + 'X' * 36).encode()

    def archive(self, extras=(), metadata=None):
        path = self.root/'molr_0.1.0_linux_amd64.tar.gz'
        data = {n: b'public fixture' for n in safety.POLICY['releaseFiles']}
        data['release.json'] = json.dumps(metadata or {'version':'0.1.0','commit':'a'*40,
            'builtAt':'2026-10-08T00:00:00.000Z','os':'linux','arch':'amd64'}).encode()
        with tarfile.open(path, 'w:gz') as out:
            for name, content in data.items():
                info = tarfile.TarInfo('molr/'+name);info.size=len(content)
                out.addfile(info, io.BytesIO(content))
            for info, content in extras:
                out.addfile(info, io.BytesIO(content) if content is not None else None)
        return path

    def test_ignore_private_runtime_files_but_keep_examples(self):
        self.repo()
        for name in ['.env.local','runtime/configs/production.json','runtime.db','runtime.db-wal',
                     'backup.sqlite3','cert.pem','private.key','credentials.json','owner-password',
                     'delivery-login.json','.cache/acceptance/config.json','runtime/data/files/user.txt',
                     'internal-docs/plan.md','apps/website/internal-docs/notes.md']:
            with self.subTest(name=name):
                result=subprocess.run(['git','-C',str(self.root),'check-ignore','--no-index','-q',name])
                self.assertEqual(result.returncode,0)
        for name in ['.env.example','apps/website/.env.example','runtime/configs/server.local.example.json','docs/README.md']:
            with self.subTest(name=name):
                result=subprocess.run(['git','-C',str(self.root),'check-ignore','--no-index','-q',name])
                self.assertEqual(result.returncode,1)

    def test_force_added_ignored_file_is_rejected(self):
        self.repo();(self.root/'.env').write_bytes(self.credential())
        safety.git('add','-f','.env',root=self.root)
        with self.assertRaisesRegex(safety.UnsafeRelease,'environment'):
            safety.check_source(self.root)

    def test_internal_documents_are_private_even_when_forced_into_git(self):
        self.repo()
        directory=self.root/'internal-docs';directory.mkdir()
        (directory/'plan.md').write_text('private planning sentinel')
        safety.check_source(self.root)
        output,_=safety.build_source('HEAD',self.root/'safe-export',self.root)
        with tarfile.open(output) as archive:
            self.assertFalse(any('internal-docs' in member.name for member in archive))
        safety.git('add','-f','internal-docs/plan.md',root=self.root)
        with self.assertRaisesRegex(safety.UnsafeRelease,'private/generated directory'):
            safety.check_source(self.root)
        self.commit()
        with self.assertRaisesRegex(safety.UnsafeRelease,'private/generated directory'):
            safety.build_source('HEAD',self.root/'unsafe-export',self.root)
        generated=self.root/'frontend/internal-docs';generated.mkdir(parents=True)
        (generated/'plan.md').write_text('private planning sentinel')
        with self.assertRaisesRegex(safety.UnsafeRelease,'private/generated directory'):
            safety.check_frontend(self.root/'frontend')

    def test_staged_credential_cannot_be_hidden_by_clean_working_file(self):
        self.repo();(self.root/'README.md').write_bytes(self.credential())
        safety.git('add','README.md',root=self.root)
        (self.root/'README.md').write_text('Clean working file')
        with self.assertRaises(safety.UnsafeRelease) as caught:
            safety.check_source(self.root)
        self.assertNotIn(self.credential().decode(),str(caught.exception))
        self.assertIn('possible provider credential',str(caught.exception))

    def test_untracked_public_source_is_checked(self):
        self.repo();(self.root/'notes.txt').write_bytes(self.credential())
        with self.assertRaisesRegex(safety.UnsafeRelease,'credential'):
            safety.check_source(self.root)

    def test_source_symlink_is_not_followed(self):
        self.repo();(self.root/'notes.txt').symlink_to('/does-not-exist')
        with self.assertRaisesRegex(safety.UnsafeRelease,'symbolic link'):
            safety.check_source(self.root)

    def test_database_with_unexpected_extension_is_detected(self):
        with self.assertRaisesRegex(safety.UnsafeRelease,'SQLite'):
            safety.check_bytes('public.bin',b'SQLite format 3\x00'+b'fixture')

    def test_private_key_marker_is_detected_without_printing_contents(self):
        data=('-----BEGIN '+ 'PRIVATE KEY-----\nsynthetic').encode()
        with self.assertRaisesRegex(safety.UnsafeRelease,'private key') as caught:
            safety.check_bytes('readme.txt',data)
        self.assertNotIn('synthetic',str(caught.exception))

    def test_saved_login_link_in_document_is_rejected(self):
        data=('http://example.invalid/#/login?token=' + 'x' * 43).encode()
        with self.assertRaisesRegex(safety.UnsafeRelease,'saved login link') as caught:
            safety.check_bytes('acceptance.md',data)
        self.assertNotIn('x' * 43,str(caught.exception))

    def test_credential_in_filename_is_redacted_in_diagnostics(self):
        name = self.credential().decode()+'.pem'
        with self.assertRaises(safety.UnsafeRelease) as caught:
            safety.check_bytes(name,b'fixture')
        self.assertNotIn(self.credential().decode(),str(caught.exception))
        self.assertIn('[REDACTED]',str(caught.exception))

    def test_source_export_uses_commit_not_index_or_local_data(self):
        self.repo()
        (self.root/'README.md').write_bytes(self.credential())
        (self.root/'new.txt').write_text('staged only')
        safety.git('add','new.txt',root=self.root)
        (self.root/'.env').write_text('local private sentinel')
        (self.root/'.cache').mkdir();(self.root/'.cache/runtime.db').write_text('database sentinel')
        path,count=safety.build_source('HEAD',self.root/'export',self.root)
        with tarfile.open(path) as archive:
            names={m.name for m in archive if m.isfile()}
            self.assertEqual(names,{'molr-source/README.md','molr-source/.gitignore','molr-source/.env.example'})
            self.assertEqual(archive.extractfile('molr-source/README.md').read(),b'Committed public source\n')
        self.assertEqual(count,3)
        self.assertTrue(path.with_name(path.name+'.sha256').is_file())

    def test_bad_committed_source_is_blocked_before_export(self):
        self.repo();(self.root/'server.json').write_text('{}')
        safety.git('add','-f','server.json',root=self.root);self.commit()
        with self.assertRaisesRegex(safety.UnsafeRelease,'configuration'):
            safety.build_source('HEAD',self.root/'export',self.root)
        self.assertFalse((self.root/'export').exists())

    def test_unlisted_public_file_and_link_are_rejected(self):
        directory=self.root/'packages/ui/public';directory.mkdir(parents=True)
        (directory/'accidental.txt').write_text('private sentinel')
        with self.assertRaisesRegex(safety.UnsafeRelease,'reviewed publicFiles'):
            safety.check_public(directory,self.root)
        (directory/'accidental.txt').unlink()
        (directory/'theme-init.js').symlink_to('/does-not-exist')
        with self.assertRaisesRegex(safety.UnsafeRelease,'symbolic link'):
            safety.check_public(directory,self.root)

    def test_reviewed_public_asset_is_accepted(self):
        directory=self.root/'packages/ui/public';directory.mkdir(parents=True)
        (directory/'theme-init.js').write_text('/* public fixture */')
        self.assertEqual(safety.check_public(directory,self.root),1)

    def test_release_requires_exact_member_set(self):
        self.assertEqual(safety.check_release(self.archive())['arch'],'amd64')
        extra=tarfile.TarInfo('molr/data/runtime.db');extra.size=7
        with self.assertRaisesRegex(safety.UnsafeRelease,'unexpected release member'):
            safety.check_release(self.archive([(extra,b'private')]))

    def test_release_rejects_links_traversal_and_duplicate_members(self):
        for name,kind in [('molr/secret',tarfile.SYMTYPE),('../escape',tarfile.REGTYPE),('molr/LICENSE',tarfile.REGTYPE)]:
            with self.subTest(name=name):
                extra=tarfile.TarInfo(name);extra.type=kind;extra.linkname='/private'
                with self.assertRaises(safety.UnsafeRelease):
                    safety.check_release(self.archive([(extra,b'' if kind==tarfile.REGTYPE else None)]))

    def test_release_metadata_must_not_contain_extra_private_fields(self):
        meta={'version':'0.1.0','commit':'a'*40,'builtAt':'2026-10-08T00:00:00.000Z',
              'os':'linux','arch':'amd64','password':'private sentinel'}
        with self.assertRaisesRegex(safety.UnsafeRelease,'metadata fields') as caught:
            safety.check_release(self.archive(metadata=meta))
        self.assertNotIn('private sentinel',str(caught.exception))

    def test_download_directory_ignores_unselected_files_but_published_output_does_not(self):
        path=self.archive();digest=hashlib.sha256(path.read_bytes()).hexdigest()
        path.with_name(path.name+'.sha256').write_text(digest+'  '+path.name+'\n')
        (self.root/'latest-linux-amd64.json').write_text(json.dumps({'version':'0.1.0','commit':'a'*40,'published_at':'2026-10-11T00:00:00Z','notes_url':'https://example.com/releases/0.1.0','file':path.name,'sha256':digest}))
        (self.root/'private.txt').write_text('private sentinel')
        self.assertEqual(safety.check_release_directory(self.root),3)
        with self.assertRaisesRegex(safety.UnsafeRelease,'unexpected file'):
            safety.check_release_directory(self.root,strict=True)

    def test_download_manifest_and_checksum_are_validated(self):
        path=self.archive()
        path.with_name(path.name+'.sha256').write_text('0'*64+'  '+path.name+'\n')
        with self.assertRaisesRegex(safety.UnsafeRelease,'checksum'):
            safety.check_release_directory(self.root)
        path.with_name(path.name+'.sha256').unlink()
        (self.root/'latest-linux-amd64.json').write_text(json.dumps({'version':'0.1.0','commit':'a'*40,'published_at':'2026-10-11T00:00:00Z','notes_url':'https://example.com/releases/0.1.0','file':path.name,'sha256':'0'*64}))
        with self.assertRaisesRegex(safety.UnsafeRelease,'checksum'):
            safety.check_release_directory(self.root)

    def test_frontend_blocks_private_files_in_output(self):
        (self.root/'index.html').write_text('<html>public</html>')
        self.assertEqual(safety.check_frontend(self.root),1)
        (self.root/'.env.local').write_text('private sentinel')
        with self.assertRaisesRegex(safety.UnsafeRelease,'environment'):
            safety.check_frontend(self.root)


if __name__=='__main__':
    unittest.main(verbosity=2)
