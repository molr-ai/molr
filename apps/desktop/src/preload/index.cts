import electron = require('electron');
import type { DesktopAPI } from '../shared.js';
const { contextBridge, ipcRenderer } = electron;
const api: DesktopAPI = {
  minimize: () => ipcRenderer.invoke('desktop:minimize'),
  toggleMaximize: () => ipcRenderer.invoke('desktop:toggle-maximize'),
  close: () => ipcRenderer.invoke('desktop:close'),
  state: () => ipcRenderer.invoke('desktop:state'),
  save: input => ipcRenderer.invoke('desktop:save', input),
  remove: id => ipcRenderer.invoke('desktop:remove', id),
  connect: id => ipcRenderer.invoke('desktop:connect', id),
  manager: () => ipcRenderer.invoke('desktop:manager'),
  refreshLocal: () => ipcRenderer.invoke('desktop:local-status'),
  localAction: (action, preset) => ipcRenderer.invoke('desktop:local-action', action, preset),
  openLogs: () => ipcRenderer.invoke('desktop:logs'),
  openDownloads: () => ipcRenderer.invoke('desktop:downloads'),
  subscribe: callback => {
    const listener = (_event: Electron.IpcRendererEvent, value: Parameters<Parameters<DesktopAPI['subscribe']>[0]>[0]) => callback(value);
    ipcRenderer.on('desktop:changed', listener);
    return () => ipcRenderer.removeListener('desktop:changed', listener);
  },
};
contextBridge.exposeInMainWorld('molrDesktop', api);
if (process.isMainFrame && process.platform === 'darwin') {
  // Page zoom changes the viewport. Main reads the real zoom itself.
  window.addEventListener('resize', () => { void ipcRenderer.invoke('desktop:sync-titlebar').catch(() => {}); });
}
