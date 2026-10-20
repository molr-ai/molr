import electron = require('electron');
import type { MolrAppAPI, AppWindowState } from '@molr/ui-host/app';
const { contextBridge, ipcRenderer } = electron;
// No Node/IPC handles or event objects cross into the page. Subframes never
// receive this bridge, including Runtime plugin and custom UI frames.
if (process.isMainFrame) {
  const api: MolrAppAPI = {
    notifications: {
      begin: scope => ipcRenderer.invoke('app:notification-begin', scope),
      requestPermission: () => ipcRenderer.invoke('app:notification-permission'),
      show: (scope, value) => ipcRenderer.invoke('app:notification-show', scope, value),
      close: (scope, tag) => ipcRenderer.invoke('app:notification-close', scope, tag),
      onClick: callback => {
        const listener = (_event: Electron.IpcRendererEvent, scope: string, route: string) => callback(scope, route);
        ipcRenderer.on('app:notification-click', listener);
        return () => ipcRenderer.removeListener('app:notification-click', listener);
      },
    },
    oauth: {
      authorize: provider => ipcRenderer.invoke('app:oauth-authorize', provider),
      cancel: () => ipcRenderer.invoke('app:oauth-cancel'),
    },
    getInfo: () => ipcRenderer.invoke('app:info'),
    window: {
      minimize: () => ipcRenderer.invoke('app:minimize'),
      toggleMaximize: () => ipcRenderer.invoke('app:toggle-maximize'),
      close: () => ipcRenderer.invoke('app:close'),
    },
    disconnect: () => ipcRenderer.invoke('app:disconnect'),
    onWindowStateChanged: callback => {
      const listener = (_event: Electron.IpcRendererEvent, state: AppWindowState) => callback(state);
      ipcRenderer.on('app:window-state', listener);
      return () => ipcRenderer.removeListener('app:window-state', listener);
    },
  };
  contextBridge.exposeInMainWorld('molrApp', api);
}
