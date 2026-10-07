import type { ReactNode } from 'react';
import { Badge } from './ui';
import { Progress } from './feedback';
import type { ProgressProps, Tone } from './feedback';

type Status = { label: string; tone?: Tone };
export type ResourceCardProps = { name: string; description?: string; icon?: ReactNode; status?: Status; metadata?: ReactNode; action?: ReactNode; className?: string };
export function ResourceCard({ name, description, icon, status, metadata, action, className = '' }: ResourceCardProps) {
  return <article className={`card ui-resource-card ${className}`}>
    <div className="ui-resource-card__heading">{icon && <span className="ui-resource-card__icon" aria-hidden="true">{icon}</span>}{status && <Badge tone={status.tone}>{status.label}</Badge>}</div>
    <h3>{name}</h3>{description && <p>{description}</p>}
    {(metadata || action) && <footer><div>{metadata}</div>{action}</footer>}
  </article>;
}

export type TaskCardProps = { title: string; description?: string; status: Status; progress?: ProgressProps; metadata?: ReactNode; actions?: ReactNode; className?: string };
export function TaskCard({ title, description, status, progress, metadata, actions, className = '' }: TaskCardProps) {
  return <article className={`card ui-task-card ${className}`}>
    <div className="ui-task-card__heading"><h3>{title}</h3><Badge tone={status.tone}>{status.label}</Badge></div>
    {description && <p>{description}</p>}{progress && <Progress {...progress} />}
    {(metadata || actions) && <footer><div>{metadata}</div><div className="ui-task-card__actions">{actions}</div></footer>}
  </article>;
}
