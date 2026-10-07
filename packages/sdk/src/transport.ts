/** Transport adapters must not expose authentication credentials to UI packages. */
export type RuntimeTransport = <T>(path: string, init: RequestInit, binary: boolean) => Promise<T>;
