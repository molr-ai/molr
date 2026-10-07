#!/usr/bin/env python3
"""Exercise tools through the authenticated sandbox API; no tool runs on host.
Usage: check-host-tools.py PRIVATE_CONNECTION_JSON OUTPUT_JSON
"""
import datetime, hashlib, http.client, json, pathlib, ssl, sys, time, uuid
from urllib.parse import urlsplit
settings=json.loads(pathlib.Path(sys.argv[1]).read_text())
endpoint=urlsplit(settings['endpoint'])
# TLS is accepted only after verifying the explicitly enrolled fingerprint.
def call(method,body=None):
 context=ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT);context.check_hostname=False;context.verify_mode=ssl.CERT_NONE
 conn=http.client.HTTPSConnection(endpoint.hostname,endpoint.port or 443,context=context,timeout=10)
 conn.connect()
 if hashlib.sha256(conn.sock.getpeercert(binary_form=True)).hexdigest()!=settings['fingerprint'].lower():
  conn.close();raise RuntimeError('TLS fingerprint mismatch')
 headers={'Authorization':'Bearer '+settings['token']}
 if body is not None:headers['Content-Type']='application/json'
 conn.request('GET' if body is None else 'POST','/v2/sandbox/'+method,body=None if body is None else json.dumps(body),headers=headers)
 res=conn.getresponse(); data=json.loads(res.read());conn.close()
 if res.status!=200 or data.get('Code'):raise RuntimeError('sandbox API error: '+json.dumps(data))
 return data.get('Data')
info=call('info'); caps=info['Capabilities']
assert info['Enabled'] and caps['Reason']=='available'
assert all(caps[k] for k in ['Shell','ProcessIsolation','FileIsolation','NetworkIsolation','ResourceLimits'])
def execute(script,network=False):
 ident={'OwnerID':'host-tools-check','AssistantID':'tools-check','ExecutionID':uuid.uuid4().hex,'OperationID':uuid.uuid4().hex}
 q={'Identity':ident,'Method':'shell_execute','Arguments':{'script':script,'directory':'/work','timeout_seconds':35},'Compute':True,'Networks':['preset:web'] if network else [],'Epoch':0}
 result=call('submit',q); deadline=time.monotonic()+45;renew=time.monotonic()+3
 while result['State']=='running':
  if time.monotonic()>deadline:call('cancel',ident);raise RuntimeError('test deadline exceeded')
  time.sleep(.2)
  if time.monotonic()>renew:call('renew',ident);renew=time.monotonic()+3
  result=call('get',ident)
 if result.get('Code'):return {'State':result['State'],'Error':result}
 return result['Result']
tests=[
 ('curl/TLS/CA','curl',"curl -q -fsSL --proto '=https' --proto-redir '=https' --max-time 15 -o /work/https.html https://www.baidu.com && test -s /work/https.html",True),
 ('Python stdlib','python3',"python3 -c 'import ssl,json,sqlite3; print(json.dumps({\"ok\":True}))'",False),
 ('pip','pip3','pip3 --version',False),
 ('pip workspace install','pip3',"target=$(mktemp -d /work/pip-probe.XXXXXX); pip3 install --no-index --target \"$target\" /work/molr_probe-1.0-py3-none-any.whl && PYTHONPATH=\"$target\" python3 -c 'import molr_probe; assert molr_probe.value==42'",False),
 ('Python venv + pip','python3','python3 -m venv /work/check-venv && /work/check-venv/bin/python -m pip --version',False),
 ('jq','jq',"printf '{\"value\":42}' | jq -e '.value==42'",False),
 ('grep','grep',"printf 'hello\\n' | grep '^hello$'",False),
 ('ripgrep','rg',"printf 'hello\\n' > /work/search.txt; rg --threads 2 hello /work/search.txt",False),
 ('ImageMagick convert/identify','convert',"convert -size 16x16 xc:white /work/tool.png && identify /work/tool.png",False),
 ('Tesseract OCR + chi_sim','tesseract',"tesseract --list-langs; tesseract /work/tool.png stdout -l chi_sim+eng --psm 7",False),
 ('ExifTool','exiftool','exiftool -ImageWidth /work/tool.png',False),
 ('Poppler PDF','pdftotext',"pdftotext /work/check.pdf /work/check.txt && pdfinfo /work/check.pdf && grep 'Sandbox PDF' /work/check.txt",False),
 ('dig','dig','dig +time=3 +tries=1 +short www.baidu.com A',True),
 ('traceroute','traceroute','traceroute -m 2 -q 1 -w 1 www.baidu.com',True),
 ('OpenSSL','openssl',"printf hello | openssl dgst -sha256",False),
 ('Git','git',"git init -q /work/check-repo && git -C /work/check-repo -c user.name=Sandbox -c user.email=sandbox@example.invalid commit --allow-empty -qm check && git -C /work/check-repo log -1 --format=%s",False),
 ('zip/unzip','zip',"printf zip-ok > /work/zip.txt; zip -q /work/tool.zip /work/zip.txt && unzip -t /work/tool.zip",False),
 ('fd','fd',"fd --threads 2 '^search.txt$' /work",False),
 ('fd Debian name','fdfind',"fdfind --threads 2 '^search.txt$' /work",False),
 ('tree','tree','tree -L 1 /work',False),
 ('less','less','less -F /work/search.txt',False),
 ('sqlite3','sqlite3',"sqlite3 /work/check.db 'create table if not exists t(v); insert into t values(42); select v from t;'",False),
 ('no sudo','sudo',"if sudo -n /usr/bin/id; then exit 1; else printf 'privilege escalation denied\\n'; fi",False),
 ('shared programs read-only','sh',"if touch /usr/molr-host-write-test; then exit 1; fi; test ! -e /etc/shadow; test ! -e /run/docker.sock; test ! -e /home; grep -E 'NoNewPrivs|CapEff' /proc/self/status",False),
]
# Construct a tiny valid PDF using Python's standard library inside the sandbox.
setup="""python3 - <<'INNER'
from pathlib import Path
stream=b'BT /F1 18 Tf 50 100 Td (Sandbox PDF) Tj ET'
objects=[b'<< /Type /Catalog /Pages 2 0 R >>',b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',b'<< /Length '+str(len(stream)).encode()+b' >>\\nstream\\n'+stream+b'\\nendstream']
data=bytearray(b'%PDF-1.4\\n');offsets=[0]
for i,o in enumerate(objects,1):offsets.append(len(data));data.extend(str(i).encode()+b' 0 obj\\n'+o+b'\\nendobj\\n')
x=len(data);data.extend(b'xref\\n0 6\\n0000000000 65535 f \\n')
for o in offsets[1:]:data.extend(f'{o:010d} 00000 n \\n'.encode())
data.extend(b'trailer\\n<< /Size 6 /Root 1 0 R >>\\nstartxref\\n'+str(x).encode()+b'\\n%%EOF\\n');Path('/work/check.pdf').write_bytes(data)

import zipfile
with zipfile.ZipFile('/work/molr_probe-1.0-py3-none-any.whl','w') as z:
 z.writestr('molr_probe.py','value=42\\n')
 z.writestr('molr_probe-1.0.dist-info/METADATA','Metadata-Version: 2.1\\nName: molr-probe\\nVersion: 1.0\\n')
 z.writestr('molr_probe-1.0.dist-info/WHEEL','Wheel-Version: 1.0\\nGenerator: acceptance\\nRoot-Is-Purelib: true\\nTag: py3-none-any\\n')
 z.writestr('molr_probe-1.0.dist-info/RECORD','')
INNER"""
rows=[{'Name':'PDF fixture','Result':execute(setup)}]
for name,command,script,network in tests:
 result=execute('command -v '+command+' >/dev/null 2>&1 || { echo tool-not-installed; exit 127; }; '+script,network)
 code=result.get('Exit',{}).get('Code')
 status='passed' if code==0 else ('not_installed' if code==127 and result.get('Stdout','').strip()=='tool-not-installed' else 'failed')
 rows.append({'Name':name,'Command':command,'Status':status,'Result':result});print(name,json.dumps(result,ensure_ascii=False),flush=True)
report={'CheckedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'Info':info,'Tests':rows}
pathlib.Path(sys.argv[2]).write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
