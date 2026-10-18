/** Narrow native capabilities available only to the active Runtime main frame. */
export type AppWindowState = { maximized: boolean; fullscreen: boolean };
export type AppInfo = {
  apiVersion: 1;
  platform: string;
  connection: { name: string; url: string };
  window: AppWindowState;
};
export type MolrAppAPI = {
  getInfo(): Promise<AppInfo>;
  window: {
    minimize(): Promise<void>;
    toggleMaximize(): Promise<void>;
    close(): Promise<void>;
  };
  disconnect(): Promise<void>;
  onWindowStateChanged(callback: (state: AppWindowState) => void): () => void;
};
