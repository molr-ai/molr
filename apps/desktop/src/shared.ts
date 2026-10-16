export type Connection = { id: string; name: string; url: string; kind: 'remote' | 'managed-local'; allowHTTP: boolean };
export type ConnectionInput = { id?: string; name: string; url: string; allowHTTP: boolean };
export type LocalStatus = {
  available: boolean; installed: boolean; state: string; health?: string; version?: string;
  url?: string; record?: string; error?: string; bundledVersion?: string;
};
export type LocalAction = 'install' | 'start' | 'stop' | 'restart' | 'upgrade' | 'uninstall';
export type DesktopState = {
  connections: Connection[]; lastConnection?: string; active?: string;
  view: 'manager' | 'loading' | 'runtime' | 'failed'; error?: string;
  local: LocalStatus; busy?: string; progress?: string; version: string; platform: string;
};
export type DesktopAPI = {
  state(): Promise<DesktopState>;
  save(input: ConnectionInput): Promise<void>;
  remove(id: string): Promise<void>;
  connect(id: string): Promise<void>;
  manager(): Promise<void>;
  refreshLocal(): Promise<void>;
  localAction(action: LocalAction): Promise<void>;
  openLogs(): Promise<void>;
  openDownloads(): Promise<void>;
  subscribe(callback: (state: DesktopState) => void): () => void;
};
