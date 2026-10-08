import type { SVGProps } from 'react';

export function UIIcon({ name, ...props }: SVGProps<SVGSVGElement> & { name: 'copy' | 'close' | 'search' | 'check' | 'minus' | 'chevron-left' | 'chevron-right' | 'chevron-down' | 'info' | 'warning' | 'arrow-up' | 'arrow-down' | 'arrow-up-right' | 'filter' | 'paperclip' | 'mic' | 'stop' | 'thumbs-up' | 'thumbs-down' | 'tool' | 'sparkles' | 'clock' | 'calendar' | 'lock' }) {
  const paths = {
    lock: 'M7 10V7a5 5 0 0 1 10 0v3M6 10h12a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2ZM12 15v3',
    copy: 'M9 9h12v12H9ZM5 15H3V3h12v2',
    close: 'm6 6 12 12M6 18 18 6', search: 'm21 21-4.5-4.5', check: 'm5 12 4 4L19 6', minus: 'M5 12h14',
    'chevron-left': 'm14 6-6 6 6 6', 'chevron-right': 'm10 6 6 6-6 6', 'chevron-down': 'm6 9 6 6 6-6',
    info: 'M12 11v6M12 7h.01', warning: 'M12 8v5M12 17h.01', 'arrow-up': 'm6 10 6-6 6 6M12 4v16', 'arrow-down': 'm6 14 6 6 6-6M12 4v16',
    paperclip: 'm21 11-8 8a6 6 0 0 1-8.5-8.5l8-8a4 4 0 0 1 5.7 5.7l-8 8a2 2 0 0 1-2.8-2.8l7.5-7.5',
    mic: 'M9 5a3 3 0 0 1 6 0v7a3 3 0 0 1-6 0ZM5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8',
    stop: 'M6 6h12v12H6Z',
    'thumbs-up': 'M7 10v11H3V10ZM7 10l5-8c2 0 3 1 2 4l-1 4h6a2 2 0 0 1 2 2l-2 7a2 2 0 0 1-2 2H7',
    'thumbs-down': 'M7 14V3H3v11ZM7 14l5 8c2 0 3-1 2-4l-1-4h6a2 2 0 0 0 2-2l-2-7a2 2 0 0 0-2-2H7',
    tool: 'M14 6a5 5 0 0 1 6-4l-3 3 2 2 3-3a5 5 0 0 1-6 6L6 20a2 2 0 0 1-3-3L13 7',
    sparkles: 'm12 4 2 6 6 2-6 2-2 6-2-6-6-2 6-2ZM20 2v4M18 4h4M4 18v4M2 20h4',
    calendar: 'M8 2v4M16 2v4M3 10h18M5 4h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z',
    clock: 'M12 7v5l3 2', 'arrow-up-right': 'M7 7h10v10M7 17 17 7', filter: 'M3 6h18M7 12h10M10 18h4',
  };
  return <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>
    {name === 'search' && <circle cx="10.5" cy="10.5" r="6.5" />}
    {name === 'info' && <circle cx="12" cy="12" r="9" />}
    {name === 'clock' && <circle cx="12" cy="12" r="9" />}
    {name === 'warning' && <path d="M10.3 3.8 2.5 17.3A2 2 0 0 0 4.2 20h15.6a2 2 0 0 0 1.7-2.7L13.7 3.8a2 2 0 0 0-3.4 0Z" />}
    <path d={paths[name]} />
  </svg>;
}
