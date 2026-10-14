import type { CSSProperties, ReactNode } from 'react';
import { AssistantAvatar } from './AssistantAvatar';

export type AvatarProps = { name: string; avatar?: string; size?: number; selected?: boolean; notice?: ReactNode; className?: string };
export type AvatarNoticeProps = { label: string; tone?: 'warning' | 'error'; className?: string };

export function Avatar({ name, avatar, size = 44, selected = false, notice, className = '' }: AvatarProps) {
 return <span className={`ui-avatar ${className}`} data-selected={selected || undefined} style={{ '--avatar-size': `${size}px` } as CSSProperties}>
  <AssistantAvatar name={name} avatar={avatar} />{notice}
 </span>;
}

export function AvatarNotice({ label, tone = 'warning', className = '' }: AvatarNoticeProps) {
 return <span className={`ui-avatar__notice ${className}`} data-tone={tone} role="img" title={label} aria-label={label} />;
}
