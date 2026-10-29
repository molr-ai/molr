import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '@molr/sdk';
import { poll } from '../../../polling';
import type { PollDelay } from '../../../polling';

type Pairing = { ID: string; ExpiresAt: string; Status: string };

/** The component owns a pairing until it confirms, cancels or unmounts. */
export function useChannelPairing<T extends Pairing>(assistantID: string, channel: string) {
  const [login, update] = useState<T | null>(null);
  const active = useRef<T | null>(null);
  const setLogin = useCallback((value: T | null) => { active.current = value; update(value); }, []);
  useEffect(() => () => {
    const current = active.current;
    active.current = null;
    if (current) void api.post(`/api/${channel}/cancel`, { AssistantID: assistantID, ID: current.ID }).catch(() => {});
  }, [assistantID, channel]);
  return { login, setLogin };
}

/** Protocol adapters decide when to poll and what each response means. */
export function usePairingPoll<T extends Pairing>({ login, enabled, read, receive }: {
  login: T | null; enabled: boolean;
  read: (signal: AbortSignal) => Promise<T>;
  receive: (next: T) => PollDelay;
}) {
  const [expiredID, setExpiredID] = useState(''), [error, setError] = useState<unknown>(null);
  const callbacks = useRef({ read, receive }); callbacks.current = { read, receive };
  const expired = !!login && expiredID === login.ID;
  useEffect(() => {
    setExpiredID(''); setError(null);
    if (!login) return;
    const timer = setTimeout(() => setExpiredID(login.ID), Math.max(0, Date.parse(login.ExpiresAt) - Date.now()));
    return () => clearTimeout(timer);
  }, [login?.ID, login?.ExpiresAt]);
  useEffect(() => {
    if (!login || !enabled || expired) return;
    const loop = poll(async signal => {
      try {
        const next = await callbacks.current.read(signal);
        if (signal.aborted) return false;
        setError(null);
        return callbacks.current.receive(next);
      } catch (error) {
        if (!signal.aborted) setError(error);
        throw error;
      }
    });
    return () => loop.stop();
  }, [login?.ID, login?.Status, enabled, expired]);
  return { expired, error };
}
