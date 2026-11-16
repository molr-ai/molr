import type { FileInfo } from '@molr/sdk';
import { useCachedFiles } from '../../../use-cached-files';

export function useFilePreviews(files: FileInfo[]) {
  return useCachedFiles(files.map(file => file.ID)).attachments;
}
