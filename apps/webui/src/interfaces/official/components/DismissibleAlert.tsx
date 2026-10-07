import { useState } from 'react';
import { Alert, type AlertProps } from '@molr/ui';

// Polling the same notice must not undo dismissal. A new cause opens it again.
export function DismissibleAlert({ noticeKey, ...props }: Omit<AlertProps, 'onDismiss'> & { noticeKey: string }) {
  return <Notice key={noticeKey} {...props} />;
}

function Notice(props: Omit<AlertProps, 'onDismiss'>) {
  const [dismissed, setDismissed] = useState(false);
  return dismissed ? null : <Alert {...props} onDismiss={() => setDismissed(true)} />;
}
