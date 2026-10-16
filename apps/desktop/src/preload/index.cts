import electron = require('electron');
import type { DesktopAPI } from '../shared.js';
const { contextBridge, ipcRenderer } = electron;
const api: DesktopAPI = {
  state: () => ipcRenderer.invoke('desktop:state'),
  save: input => ipcRenderer.invoke('desktop:save', input),
  remove: id => ipcRenderer.invoke('desktop:remove', id),
  connect: id => ipcRenderer.invoke('desktop:connect', id),
  manager: () => ipcRenderer.invoke('desktop:manager'),
  refreshLocal: () => ipcRenderer.invoke('desktop:local-status'),
  localAction: action => ipcRenderer.invoke('desktop:local-action', action),
  openLogs: () => ipcRenderer.invoke('desktop:logs'),
  openDownloads: () => ipcRenderer.invoke('desktop:downloads'),
  subscribe: callback => {
    const listener = (_event: Electron.IpcRendererEvent, value: Parameters<Parameters<DesktopAPI['subscribe']>[0]>[0]) => callback(value);
    ipcRenderer.on('desktop:changed', listener);
    return () => ipcRenderer.removeListener('desktop:changed', listener);
  },
};
contextBridge.exposeInMainWorld('molrDesktop', api);
