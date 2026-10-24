import { createHash } from 'node:crypto';
import { appendFileSync, readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { compareVersions, releaseChannel } from './release-lib.mjs';

const indexType = 'application/vnd.oci.image.index.v1+json';
const manifestType = 'application/vnd.oci.image.manifest.v1+json';
const dockerManifestType = 'application/vnd.docker.distribution.manifest.v2+json';
const digestPattern = /^sha256:[a-f0-9]{64}$/;
const sha = bytes => 'sha256:' + createHash('sha256').update(bytes).digest('hex');
const key = name => `org.opencontainers.image.${name}`;

export function identity(repository, version, commit, created) {
  if (!/^[a-z0-9][a-z0-9_.-]*\/[a-z0-9][a-z0-9_.-]*$/.test(repository) || !/^[a-f0-9]{40}$/.test(commit) || !Number.isFinite(Date.parse(created))) throw new Error('Invalid container identity');
  return { repository, version, commit, created, channel: releaseChannel(version), image: `ghcr.io/${repository}` };
}

export function validateIndex(value, expected) {
  const a = value?.annotations;
  if (value?.schemaVersion !== 2 || value.mediaType !== indexType || !a || !/^[a-f0-9]{40}$/.test(a[key('revision')]) || !Number.isFinite(Date.parse(a[key('created')]))) throw new Error('Invalid container index');
  releaseChannel(a[key('version')]);
  if (expected && (a[key('version')] !== expected.version || a[key('revision')] !== expected.commit || a[key('source')] !== `https://github.com/${expected.repository}`)) throw new Error('Published container identity mismatch');
  if (value.manifests?.length !== 2 || value.manifests.some(d => d.platform?.os !== 'linux' || !digestPattern.test(d.digest) || !Number.isSafeInteger(d.size) || d.size <= 0 || ![manifestType, dockerManifestType].includes(d.mediaType)) || value.manifests.map(d => d.platform.architecture).sort().join(',') !== 'amd64,arm64') throw new Error('Incomplete container platforms');
  return value;
}

// Only 404 means absent. Authentication and transient failures must stop publication.
export async function registry(repository, { username, password, fetcher = fetch } = {}) {
  const scope = `repository:${repository}:${password ? 'pull,push' : 'pull'}`;
  const headers = password ? { Authorization: 'Basic ' + Buffer.from(`${username}:${password}`).toString('base64') } : {};
  const tokenResponse = await fetcher(`https://ghcr.io/token?service=ghcr.io&scope=${encodeURIComponent(scope)}`, { headers, signal: AbortSignal.timeout(30000), redirect: 'error' });
  if (!tokenResponse.ok) throw new Error(`Registry authentication failed: ${tokenResponse.status}`);
  const { token } = await tokenResponse.json();
  if (typeof token !== 'string' || !token) throw new Error('Missing registry token');
  return async (path, body) => {
    const response = await fetcher(`https://ghcr.io/v2/${repository}/${path}`, {
      method: body === undefined ? 'GET' : 'PUT', body,
      headers: { Authorization: `Bearer ${token}`, Accept: `${indexType}, ${manifestType}, ${dockerManifestType}`, ...(body === undefined ? {} : { 'Content-Type': indexType }) },
      signal: AbortSignal.timeout(30000), redirect: body === undefined ? 'follow' : 'error',
    });
    if (response.status === 404 && body === undefined) return null;
    if (!response.ok) throw new Error(`Registry ${path.split('/')[0]} failed: ${response.status}`);
    const bytes = Buffer.from(await response.arrayBuffer());
    if (body !== undefined) return;
    if (bytes.length > 4 * 1024 * 1024) throw new Error('Oversized registry response');
    const digest = sha(bytes);
    if (response.headers.get('docker-content-digest') && response.headers.get('docker-content-digest') !== digest) throw new Error('Registry digest mismatch');
    return { bytes, digest, value: JSON.parse(bytes), headers: response.headers };
  };
}

export async function plan(api, id) {
  const tag = id.channel === 'nightly' ? 'nightly' : id.version;
  const existing = await api(`manifests/${tag}`);
  if (!existing) return { build: true };
  validateIndex(existing.value);
  const version = existing.value.annotations[key('version')];
  if (id.channel === 'nightly' && releaseChannel(version) === 'nightly' && compareVersions(version, id.version) > 0) return { build: false, superseded: true };
  if (id.channel === 'nightly' && releaseChannel(version) === 'nightly' && compareVersions(version, id.version) < 0) return { build: true };
  validateIndex(existing.value, id);
  return { build: false, existing };
}

export async function publish(api, id, digests) {
  const state = await plan(api, id);
  if (state.superseded) return { skipped: 'newer nightly already published' };
  let content = state.existing;
  if (!content) {
    const manifests = [];
    for (const arch of ['amd64', 'arm64']) {
      const digest = digests?.[arch]?.digest;
      const testedConfig = digests?.[arch]?.config_digest;
      if (!digestPattern.test(digest)) throw new Error(`Missing verified ${arch} digest`);
      const manifest = await api(`manifests/${digest}`);
      if (!digestPattern.test(testedConfig) || !manifest || manifest.value.config?.digest !== testedConfig || manifest.digest !== digest || ![manifestType, dockerManifestType].includes(manifest.value.mediaType) || !digestPattern.test(manifest.value.config?.digest)) throw new Error('Invalid platform manifest');
      const config = await api(`blobs/${manifest.value.config.digest}`);
      const labels = config?.value.config?.Labels;
      if (!config || config.digest !== manifest.value.config.digest || config.value.os !== 'linux' || config.value.architecture !== arch || labels?.[key('version')] !== id.version || labels?.[key('revision')] !== id.commit || labels?.[key('source')]?.toLowerCase() !== `https://github.com/${id.repository}`) throw new Error('Platform build identity mismatch');
      manifests.push({ mediaType: manifest.value.mediaType, digest, size: manifest.bytes.length, platform: { os: 'linux', architecture: arch } });
    }
    const value = validateIndex({ schemaVersion: 2, mediaType: indexType, manifests, annotations: {
      [key('version')]: id.version, [key('revision')]: id.commit, [key('created')]: id.created,
      [key('source')]: `https://github.com/${id.repository}`, [key('licenses')]: 'MIT',
    } }, id);
    const bytes = Buffer.from(JSON.stringify(value));
    content = { bytes, value, digest: sha(bytes) };
    const tag = id.channel === 'nightly' ? 'nightly' : id.version;
    await api(`manifests/${tag}`, bytes);
    const actual = await api(`manifests/${tag}`);
    if (actual?.digest !== content.digest) throw new Error('Published index verification failed');
  }
  const tags = [id.channel === 'nightly' ? 'nightly' : id.version];
  if (id.channel === 'stable') {
    const latest = await api('manifests/latest');
    if (latest) {
      validateIndex(latest.value);
      if (releaseChannel(latest.value.annotations[key('version')]) !== 'stable') throw new Error('latest is not a stable release');
    }
    // Recover an interrupted alias update even when an older release is retried.
    let highest = id.version, last = '';
    for (let page = 0; ; page++) {
      if (page >= 100) throw new Error('Container tag scan limit exceeded');
      const list = await api(`tags/list?n=100&last=${encodeURIComponent(last)}`);
      if (!Array.isArray(list?.value.tags)) throw new Error('Invalid registry tag list');
      for (const tag of list.value.tags) {
        if (/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(tag) && compareVersions(tag, highest) > 0) highest = tag;
      }
      if (!list.headers?.get('link')) break;
      const next = list.value.tags.at(-1);
      if (typeof next !== 'string' || next <= last) throw new Error('Invalid registry pagination');
      last = next;
    }
    const recommended = highest === id.version ? content : await api(`manifests/${highest}`);
    validateIndex(recommended?.value, { ...id, version: highest, commit: recommended?.value.annotations?.[key('revision')] });
    if (!latest || compareVersions(latest.value.annotations[key('version')], highest) < 0) {
      await api('manifests/latest', recommended.bytes);
      if ((await api('manifests/latest'))?.digest !== recommended.digest) throw new Error('latest verification failed');
      if (recommended.digest === content.digest) tags.push('latest');
    } else if (latest.value.annotations[key('version')] === highest && latest.digest !== recommended.digest) throw new Error('latest conflicts with fixed version');
  }
  return { image: id.image, version: id.version, commit: id.commit, digest: content.digest, tags };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const id = identity(process.env.GITHUB_REPOSITORY.toLowerCase(), process.env.MOLR_VERSION, process.env.MOLR_COMMIT, process.env.MOLR_PUBLISHED_AT);
  const api = await registry(id.repository, { username: process.env.GITHUB_ACTOR, password: process.env.GH_TOKEN });
  if (process.argv.includes('--plan')) {
    const result = await plan(api, id);
    appendFileSync(process.env.GITHUB_OUTPUT, `build=${result.build}\nimage=${id.image}\n`);
  } else {
    const directory = process.env.MOLR_DIGEST_DIR || '.cache/container-digests';
    const digests = {};
    if (process.env.MOLR_CONTAINER_BUILD === 'true') for (const name of readdirSync(directory)) {
      if (!/^(amd64|arm64)\.json$/.test(name)) throw new Error('Unexpected digest artifact');
      digests[name.slice(0, -5)] = JSON.parse(readFileSync(resolve(directory, name)));
    }
    const result = await publish(api, id, digests);
    console.log(JSON.stringify(result));
    if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `\nContainer publication:\n\n\`\`\`json\n${JSON.stringify(result, null, 2)}\n\`\`\`\n`);
  }
}
