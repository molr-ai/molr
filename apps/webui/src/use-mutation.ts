import { useMemo } from 'react';
import type { DependencyList } from 'react';
import { createMutationOperation } from './mutation-operation';

/** Inputs identify one logical operation; changing them starts a fresh operation.
 * Include every editable input and target. Use stable state values, not inline DTOs.
 * Success releases the key; uncertain failures retain it until retry, edit or unmount.
 */
export function useMutation(inputs: DependencyList) {
  return useMemo(createMutationOperation, inputs);
}
