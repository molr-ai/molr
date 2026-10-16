import { useMemo } from 'react';
import { Alert } from '@molr/ui';
import QRCode from 'qrcode';

export function LocalQRCode({ value, label }: { value: string; label: string }) {
 const qr = useMemo(() => { try { return QRCode.create(value, { errorCorrectionLevel: 'M' }); } catch { return null; } }, [value]);
 if (!qr) return <Alert tone="danger" title={label} />;
 const size = qr.modules.size, paths: string[] = [];
 for (let row = 0; row < size; row++) for (let col = 0; col < size; col++) if (qr.modules.get(row, col)) paths.push(`M${col + 4} ${row + 4}h1v1h-1z`);
 return <svg role="img" aria-label={label} className="weixin-qr" viewBox={`0 0 ${size + 8} ${size + 8}`} shapeRendering="crispEdges"><rect width={size + 8} height={size + 8} fill="var(--color-qr-background)" /><path d={paths.join('')} fill="var(--color-qr-foreground)" /></svg>;
}
