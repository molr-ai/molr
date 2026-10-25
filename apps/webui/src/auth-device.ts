import { api, APIError, mutationKey } from '@molr/sdk';

// IndexedDB is origin-scoped, works on LAN HTTP, and serializes first-use and
// login-lock updates across tabs. Only the random device ID and a lease live here.
type DeviceRecord = { id: string; owner?: string; until?: number };
let database: Promise<IDBDatabase> | undefined;
function openDatabase() {
  return database ??= new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open('molr-auth-device', 1);
    request.onupgradeneeded = () => request.result.createObjectStore('device');
    request.onsuccess = () => { request.result.onversionchange = () => { request.result.close(); database = undefined; }; resolve(request.result); };
    request.onerror = () => { database = undefined; reject(new APIError('device_storage_unavailable')); };
  });
}
async function updateDevice(update: (record: DeviceRecord) => void): Promise<DeviceRecord> {
  let db: IDBDatabase;
  try { db = await openDatabase(); } catch { throw new APIError('device_storage_unavailable'); }
  return new Promise((resolve, reject) => {
    const tx = db.transaction('device', 'readwrite'), store = tx.objectStore('device');
    let record: DeviceRecord, failure: unknown;
    const request = store.get('current');
    request.onsuccess = () => {
      record = request.result ?? { id: mutationKey() };
      try { update(record); store.put(record, 'current'); } catch (error) { failure = error; tx.abort(); }
    };
    tx.oncomplete = () => resolve(record);
    tx.onabort = tx.onerror = () => reject(failure ?? new APIError('device_storage_unavailable'));
  });
}

export function browserDeviceName() {
  const ua = navigator.userAgent;
  const browser = /Edg\//.test(ua) ? 'Edge' : /(?:Firefox|FxiOS)\//.test(ua) ? 'Firefox' : /(?:Chrome|CriOS)\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Browser';
  const os = /iPhone/.test(ua) ? 'iPhone' : /iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) ? 'iPad' : /Android/.test(ua) ? 'Android' : /Windows/.test(ua) ? 'Windows' : /Macintosh|Mac OS X/.test(ua) ? 'Mac' : /Linux/.test(ua) ? 'Linux' : '';
  return os ? `${browser} / ${os}` : browser;
}

const leaseLifetime = 6 * 60_000;
export type LoginResponse = { challenge?: string; target_path?: string };

export class BrowserLogin {
  private owner = '';
  private device = '';
  private csrf = '';
  private challenge = '';
  private heartbeat?: number;
  private opening?: Promise<void>;
  private closeOnLeave = () => { void this.close(); };

  private async acquire() {
    if (this.owner) return this.assertLease();
    if (this.opening) return this.opening;
    this.opening = (async () => {
      const owner = mutationKey();
      const record = await updateDevice(record => {
        if (record.owner && (record.until ?? 0) > Date.now()) throw new APIError('login_in_progress');
        record.owner = owner; record.until = Date.now() + leaseLifetime;
      });
      this.owner = owner; this.device = record.id;
      window.addEventListener('pagehide', this.closeOnLeave);
      this.heartbeat = window.setInterval(() => { void this.assertLease().catch(() => this.close()); }, 30_000);
    })();
    try { await this.opening; } finally { this.opening = undefined; }
  }

  private async assertLease() {
    const owner = this.owner;
    await updateDevice(record => {
      if (!owner || record.owner !== owner || (record.until ?? 0) <= Date.now()) throw new APIError('login_interrupted');
      record.until = Date.now() + leaseLifetime;
    });
  }

  async close() {
    const owner = this.owner;
    this.owner = ''; this.challenge = '';
    window.clearInterval(this.heartbeat);
    window.removeEventListener('pagehide', this.closeOnLeave);
    if (owner) await updateDevice(record => {
      if (record.owner === owner) { delete record.owner; delete record.until; }
    }).catch(() => {});
  }

  async start<T extends LoginResponse>(path: string, input: object): Promise<T> {
    await this.acquire();
    try {
      const proof = await api.get<{ csrf_token: string }>('/api/auth/csrf');
      this.csrf = proof.csrf_token;
      const result = await api.post<T>(path, { ...input, device_id: this.device, device_name: browserDeviceName(), client_type: 'web' }, this.csrf).catch(error => {
        if (path === '/api/auth/login' && error instanceof APIError && error.code === 'unauthenticated') {
          throw new APIError('invalid_credentials', error.requestID, error.status, error.retryAt, error.reason);
        }
        throw error;
      });
      this.challenge = result.challenge ?? '';
      if (!this.challenge) await this.close();
      return result;
    } catch (error) { await this.close(); throw error; }
  }

  async complete(code: string) {
    try {
      await this.assertLease();
      if (!this.challenge) throw new APIError('login_interrupted');
      await api.post('/api/auth/mfa/complete', { challenge: this.challenge, code, device_id: this.device, client_type: 'web' }, this.csrf).catch(error => {
        if (error instanceof APIError && error.code === 'unauthenticated') {
          throw new APIError('invalid_verification_code', error.requestID, error.status, error.retryAt, error.reason);
        }
        throw error;
      });
      await this.close();
    } catch (error) {
      if (error instanceof APIError && (error.status === 409 || error.code === 'login_interrupted')) { await this.close(); throw new APIError('login_interrupted'); }
      throw error;
    }
  }

  async preview(token: string) {
    await this.acquire();
    try {
      const proof = await api.get<{ csrf_token: string }>('/api/auth/csrf');
      return await api.post<{ expires_at: string }>('/api/auth/login-links/preview', { token }, proof.csrf_token);
    } finally { await this.close(); }
  }
}
