import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { subscribeRuntime } from '@molr/sdk';
import type { RuntimeConnection } from '@molr/sdk';

export function useRuntimeConnection(onUpdate: (event: MessageEvent) => void, onRestore: () => void) {
  const [connection, setConnection] = useState<RuntimeConnection>('reconnecting');
  const callbacks = useRef({ onUpdate, onRestore }), retryRef = useRef(() => {});
  useLayoutEffect(() => { callbacks.current = { onUpdate, onRestore }; }, [onUpdate, onRestore]);
  const retryConnection = useCallback(() => retryRef.current(), []);
  useEffect(() => {
    const subscription = subscribeRuntime({ onUpdate: event => callbacks.current.onUpdate(event), onRestore: () => callbacks.current.onRestore(), onConnection: setConnection });
    retryRef.current = subscription.retry;
    return () => { retryRef.current = () => {}; subscription.dispose(); };
  }, []);
  return { connection, retryConnection };
}
