import type { WorkspaceText } from '@molr/sdk';
export type ExplorerMemory = {
  directory: string;
  selected?: string;
  scroll: number;
  hidden: boolean;
  sort: string;
  draft?: WorkspaceText & { value: string };
};
export const parent = (path: string) =>
  path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : '.';
export const join = (dir: string, name: string) =>
  dir === '.' ? name : `${dir}/${name}`;
export const size = (bytes: number) =>
  bytes < 1024
    ? `${bytes} B`
    : bytes < 1024 * 1024
      ? `${(bytes / 1024).toFixed(1)} KB`
      : `${(bytes / 1024 / 1024).toFixed(1)} MB`;
export const validName = (value: string) =>
  !!value.trim() &&
  value !== '.' &&
  value !== '..' &&
  !/[\\/\x00-\x1f]/.test(value);
