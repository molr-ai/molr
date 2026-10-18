import type { ReactNode } from 'react';

export function Icon({ name, className = '' }: { name: string; className?: string }) {
  const paths: Record<string, ReactNode> = {
    windows: <><path d="M3 5 10 4v7H3V5Zm10-1.5L21 2v9h-8V3.5ZM3 14h7v7l-7-1v-6Zm10 0h8v9l-8-1.5V14Z" /></>,
    apple: <><path d="M12 7c-2-2-6-2-8 1-2 4 1 10 4 12 1 1 3-1 4-1s3 2 4 1c2-1 4-4 4-6-4-1-4-5-1-7-2-2-5-2-7 0Z" /><path d="M12 5c0-3 2-4 4-4 0 2-1 4-4 4Z" /></>,
    linux: <><path d="M8 10V7a4 4 0 0 1 8 0v3c3 3 4 7 1 9H7c-3-2-2-6 1-9Z" /><ellipse cx="12" cy="14.5" rx="3" ry="4.5" /><path d="m10 8 2 1 2-1M5 18l-2 3h6l-1-2m11-1 2 3h-6l1-2M10 6h.01M14 6h.01" /></>,
    arrow: <path d="M5 12h14m-5-5 5 5-5 5" />,
    down: <path d="M12 5v14m-5-5 5 5 5-5" />,
    terminal: <><rect x="3" y="4" width="18" height="16" rx="2" /><path d="m7 9 3 3-3 3m6 0h4" /></>,
    container: <><path d="m3 7 5-3 13 3v11l-5 3-13-3V7Zm0 0 13 3 5-3M16 10v11M7 11v5m5-4v5" /></>,
    bot: <><rect x="4" y="7" width="16" height="14" rx="3" /><path d="M12 3v4M2 12v4m20-4v4M8 12h.01M16 12h.01M9 17h6" /><circle cx="12" cy="3" r="1" /></>,
    download: <><path d="M12 3v12m-4-4 4 4 4-4M5 16v4h14v-4" /></>,
    book: <><path d="M12 5v16M3 3h5a4 4 0 0 1 4 4 4 4 0 0 1 4-4h5v16h-5a4 4 0 0 0-4 2 4 4 0 0 0-4-2H3V3Z" /></>,
    sun: <><circle cx="12" cy="12" r="4" /><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" /></>,
    moon: <path d="M20 15.5A9 9 0 0 1 8.5 4a9 9 0 1 0 11.5 11.5Z" />,
    language: <><path d="m5 8 6 6" /><path d="m4 14 6-6 2-3" /><path d="M2 5h12" /><path d="M7 2h1" /><path d="m22 22-5-10-5 10" /><path d="M14 18h6" /></>,
    chat: <path d="M20 11a8 8 0 0 1-8 8H4l1-4a8 8 0 1 1 15-4Z" />,
    plan: <><rect x="4" y="5" width="16" height="16" rx="3" /><path d="M8 3v4m8-4v4M4 11h16m-12 4h3m3 0h2" /></>,
    folder: <path d="M3 7V5h6l2 3h10v12H3V7Z" />,
    copy: <><rect x="8" y="8" width="12" height="13" rx="2" /><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3" /></>,
    check: <path d="m5 12 4 4L19 6" />,
    file: <><path d="M14 3H5v18h14V8l-5-5Zm0 0v6h5M8 13h8m-8 4h6" /></>,
    shield: <><path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z" /><path d="m8 12 3 3 5-5" /></>,
    plus: <path d="M12 5v14M5 12h14" />,
    model: <><rect x="6" y="6" width="12" height="12" rx="3" /><path d="M9 2v4m6-4v4M9 18v4m6-4v4M2 9h4m-4 6h4m12-6h4m-4 6h4" /></>,
    server: <><rect x="4" y="3" width="16" height="7" rx="2" /><rect x="4" y="14" width="16" height="7" rx="2" /><path d="M8 6.5h.01M8 17.5h.01M12 6.5h4m-4 11h4M12 10v4" /></>,
    clock: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>,
    devices: <><path d="M12 17H3V4h16v4M5 21h6m-4-4v4" /><rect x="14" y="10" width="7" height="11" rx="1.5" /><path d="M17.5 18h.01" /></>,
    settings: <><path d="M4 7h16M4 17h16" /><circle cx="9" cy="7" r="3" /><circle cx="15" cy="17" r="3" /></>,
  };
  return <svg className={`home-icon ${className}`} width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[name]}</svg>;
}
