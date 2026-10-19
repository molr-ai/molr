import { spawnSync, execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, renameSync, lstatSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const stateName = 'notarization.json';
const credentialNames = ['APPLE_ID', 'APPLE_APP_SPECIFIC_PASSWORD', 'APPLE_TEAM_ID'];
const digest = file => createHash('sha256').update(readFileSync(file)).digest('hex');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

function writeJSON(file, value) {
  writeFileSync(`${file}.tmp`, JSON.stringify(value, null, 2) + '\n');
  renameSync(`${file}.tmp`, file);
}

function credentials(env) {
  if (credentialNames.some(name => !env[name])) throw new Error('Missing Apple notarization credentials');
  return ['--apple-id', env.APPLE_ID, '--password', env.APPLE_APP_SPECIFIC_PASSWORD, '--team-id', env.APPLE_TEAM_ID];
}

function redact(text, env) {
  for (const name of credentialNames) if (env[name]) text = text.split(env[name]).join('[redacted]');
  return text;
}

function command(file, args, { env, timeout = 120_000 }) {
  const result = spawnSync(file, args, { env, encoding: 'utf8', timeout, maxBuffer: 8 * 1024 * 1024 });
  // Never include the command line: notarytool authentication arguments are secret.
  if (result.error || result.status !== 0) {
    throw new Error(`${file} failed: ${redact(`${result.stdout || ''}\n${result.stderr || ''}`, env).slice(-4000) || result.error?.code || result.status}`);
  }
  return result.stdout;
}

function validateManifest(manifest, expected) {
  if (manifest.schema !== 1 || manifest.os !== 'darwin' || !['amd64', 'arm64'].includes(manifest.arch)
      || manifest.variant !== 'full' || typeof manifest.signed !== 'boolean'
      || !/^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(manifest.version)
      || !/^[a-f0-9]{40}$/.test(manifest.commit)
      || manifest.version !== expected.version || manifest.commit !== expected.commit || manifest.arch !== expected.arch) {
    throw new Error('Notarization artifact identity mismatch');
  }
  const name = `molr-desktop_${manifest.version}_darwin-${manifest.arch}_full.dmg`;
  if (manifest.artifacts?.length !== 1 || manifest.artifacts[0].file !== name
      || !/^[a-f0-9]{64}$/.test(manifest.artifacts[0].sha256)) throw new Error('Invalid notarization artifact');
  return name;
}

export function writeManifest(directory, manifest) {
  for (const artifact of manifest.artifacts) {
    artifact.sha256 = digest(join(directory, artifact.file));
    writeFileSync(join(directory, `${artifact.file}.sha256`), `${artifact.sha256}  ${artifact.file}\n`);
  }
  writeJSON(join(directory, 'desktop-manifest.json'), manifest);
}

export function prepareNotarization(directory, manifest, { env = process.env, run = command } = {}) {
  const filename = validateManifest(manifest, manifest);
  const file = join(directory, filename);
  if (!lstatSync(file).isFile() || digest(file) !== manifest.artifacts[0].sha256) throw new Error('Notarization artifact checksum mismatch');
  let submissionId = null;
  if (manifest.signed) {
    run('codesign', ['--verify', '--strict', file], { env });
    run('hdiutil', ['verify', file], { env });
    const response = JSON.parse(run('xcrun', ['notarytool', 'submit', file, ...credentials(env), '--output-format', 'json'], { env, timeout: 20 * 60_000 }));
    if (!/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(response.id || '')) throw new Error('Apple did not return a valid notarization submission ID');
    submissionId = response.id;
    console.log(`Notarization submitted: ${submissionId}. Save the DMG and ${stateName} before waiting.`);
  }
  writeJSON(join(directory, stateName), { schema: 1, submissionId, manifest });
}

export async function finishNotarization(directory, expected, {
  env = process.env, run = command, pause = sleep, now = Date.now, log = console.log,
  maxWaitMs = 30 * 60_000, intervalMs = 60_000,
} = {}) {
  const state = JSON.parse(readFileSync(join(directory, stateName), 'utf8'));
  if (state.schema !== 1) throw new Error('Unknown notarization state');
  const filename = validateManifest(state.manifest, expected);
  const file = join(directory, filename);
  if (!lstatSync(file).isFile() || digest(file) !== state.manifest.artifacts[0].sha256) throw new Error('Notarization artifact checksum mismatch');
  const { submissionId, manifest } = state;
  if (manifest.signed) {
    if (!/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(submissionId || '')) throw new Error('Missing notarization submission ID');
    const auth = credentials(env);
    const deadline = now() + maxWaitMs;
    let failures = 0;
    while (true) {
      let response;
      try {
        response = JSON.parse(run('xcrun', ['notarytool', 'info', submissionId, ...auth, '--output-format', 'json'], { env, timeout: 60_000 }));
        failures = 0;
      } catch (error) {
        log(`Notarization query failed (${++failures}/3): ${redact(error.message, env)}`);
        if (failures >= 3) throw new Error(`Could not query notarization ${submissionId}; rerun the failed job to resume the same submission.`);
      }
      if (response) {
        if (response.id !== submissionId) throw new Error('Apple returned a different notarization submission');
        log(`Notarization ${submissionId}: ${response.status}`);
        if (response.status === 'Accepted') break;
        if (response.status !== 'In Progress') {
          try {
            log(redact(run('xcrun', ['notarytool', 'log', submissionId, ...auth], { env, timeout: 60_000 }), env));
          } catch { log('Notarization diagnostic log unavailable.'); }
          throw new Error(`Notarization ${submissionId} was not accepted: ${response.status}`);
        }
      }
      if (now() >= deadline) throw new Error(`Notarization ${submissionId} is still pending; rerun only the failed job to resume the saved DMG and submission.`);
      await pause(Math.min(intervalMs, deadline - now()));
    }
    for (let attempt = 1; ; attempt++) {
      try { run('xcrun', ['stapler', 'staple', file], { env }); break; }
      catch (error) {
        if (attempt === 3) throw error;
        await pause(15_000);
      }
    }
    run('xcrun', ['stapler', 'validate', file], { env });
    run('spctl', ['--assess', '--type', 'open', '--context', 'context:primary-signature', '--verbose', file], { env });
  } else if (submissionId !== null) throw new Error('Unsigned artifact has a notarization submission');
  // Stapling changes the DMG, so calculate all public checksums only afterwards.
  writeManifest(directory, manifest);
  log(`Desktop installer ready: ${filename}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  if (process.platform !== 'darwin' || process.argv.length !== 3) throw new Error('Run notarization.mjs <artifact-directory> on the matching native macOS runner');
  const root = resolve(import.meta.dirname, '../../..');
  await finishNotarization(resolve(process.argv[2]), {
    version: process.env.MOLR_VERSION,
    commit: execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim(),
    arch: process.arch === 'arm64' ? 'arm64' : 'amd64',
  });
}
