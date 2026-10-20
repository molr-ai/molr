/** Narrow native capabilities available only to the active Runtime main frame. */
export type AppWindowState = { maximized: boolean; fullscreen: boolean };
export type AppAppearance = {
  theme: 'system' | 'light' | 'dark';
  palette: 'forest' | 'ocean' | 'iris' | 'sand' | 'rose' | 'monochrome';
};
export type AppOAuthResult = {
  status: 'connected' | 'cancelled' | 'failed' | 'unknown';
  attemptId?: string;
  connectionId?: string;
};
export type AppUpdateStatus = {
  comparison: 'unknown' | 'current' | 'available' | 'ahead' | 'uncomparable' | 'unavailable';
  checking: boolean;
  latestVersion?: string;
  lastSuccessAt?: string;
  nextAllowedAt?: string;
  error?: string;
  local?: { version?: string; targetVersion?: string; pending: boolean; busy: boolean; error?: string };
};
export type AppInfo = {
  apiVersion: 1;
  platform: string;
  build: { version: string; commit: string };
  connection: { name: string; url: string; kind: 'remote' | 'managed-local' };
  window: AppWindowState;
};
export type AppNotification = { tag: string; title: string; body: string; route: string };
export type MolrAppAPI = {
  updates: {
    status(): Promise<AppUpdateStatus>;
    check(): Promise<AppUpdateStatus>;
    openDownloads(): Promise<void>;
    /** Opens native confirmation; only available for the active managed local service. */
    upgradeLocal(): Promise<void>;
    onChanged(callback: (status: AppUpdateStatus) => void): () => void;
  };
  notifications: {
    begin(scope: string): Promise<void>;
    /** Initializes the OS channel on user activation; support is not permission. */
    requestPermission(): Promise<boolean>;
    show(scope: string, notification: AppNotification): Promise<void>;
    close(scope: string, tag?: string): Promise<void>;
    onClick(callback: (scope: string, route: string) => void): () => void;
  };
  oauth: {
    /** Provider IDs are registered by Runtime. Credentials never enter the page. */
    authorize(provider: string): Promise<AppOAuthResult>;
    cancel(): Promise<void>;
  };
  getInfo(): Promise<AppInfo>;
  /** Sync the signed-in account's appearance to the desktop connection manager. */
  setAppearance?(appearance: AppAppearance): Promise<void>;
  /** One-use permission for the desktop-owned installation login handoff. */
  consumeInitialLogin?(): Promise<boolean>;
  window: {
    minimize(): Promise<void>;
    toggleMaximize(): Promise<void>;
    close(): Promise<void>;
  };
  disconnect(): Promise<void>;
  onWindowStateChanged(callback: (state: AppWindowState) => void): () => void;
};
