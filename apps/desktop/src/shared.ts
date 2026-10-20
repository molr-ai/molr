import type { AppAppearance, AppUpdateStatus, AppWindowState } from '@molr/ui-host/app';
export type Connection = { id: string; name: string; url: string; kind: 'remote' | 'managed-local'; allowHTTP: boolean };
export type ConnectionInput = { id?: string; name: string; url: string; allowHTTP: boolean };
export type LocalStatus = {
  available: boolean; installed: boolean; state: string; health?: string; version?: string;
  url?: string; record?: string; error?: string; bundledVersion?: string; updatePending?: boolean;
};
export type ResourcePreset = 'permissive' | 'least-privilege';
export type AdministratorInput = { username: string; password: string };
export type LocalAction = 'install' | 'start' | 'stop' | 'restart' | 'upgrade' | 'uninstall';
export type DesktopState = {
  appearance: AppAppearance;
  connections: Connection[]; lastConnection?: string; active?: string;
  view: 'manager' | 'loading' | 'runtime' | 'failed'; error?: string;
  updates: AppUpdateStatus; local: LocalStatus; busy?: string; progress?: string; version: string; platform: string; window: AppWindowState;
};
export type DesktopAPI = {
  minimize(): Promise<void>;
  toggleMaximize(): Promise<void>;
  close(): Promise<void>;
  state(): Promise<DesktopState>;
  save(input: ConnectionInput): Promise<void>;
  remove(id: string): Promise<void>;
  connect(id: string): Promise<void>;
  manager(): Promise<void>;
  refreshLocal(): Promise<void>;
  localAction(action: LocalAction, preset?: ResourcePreset, administrator?: AdministratorInput): Promise<void>;
  openLogs(): Promise<void>;
  subscribe(callback: (state: DesktopState) => void): () => void;
};
