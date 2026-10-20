/** Narrow native capabilities available only to the active Runtime main frame. */
export type AppWindowState = { maximized: boolean; fullscreen: boolean };
export type AppOAuthResult = {
  status: 'connected' | 'cancelled' | 'failed' | 'unknown';
  attemptId?: string;
  connectionId?: string;
};
export type AppInfo = {
  apiVersion: 1;
  platform: string;
  connection: { name: string; url: string };
  window: AppWindowState;
};
export type AppNotification = { tag: string; title: string; body: string; route: string };
export type MolrAppAPI = {
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
  window: {
    minimize(): Promise<void>;
    toggleMaximize(): Promise<void>;
    close(): Promise<void>;
  };
  disconnect(): Promise<void>;
  onWindowStateChanged(callback: (state: AppWindowState) => void): () => void;
};
