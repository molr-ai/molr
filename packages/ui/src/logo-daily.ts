import { useEffect, useState } from 'react';

export function getDailyLogoPreset(date = new Date()) {
  // Select by local clock time. A night slot spans midnight, so its key
  // remains stable until 08:00 and does not restart the animation at 00:00.
  const year = date.getFullYear();
  const month = date.getMonth();
  const day = date.getDate();
  const hour = date.getHours();
  const period = hour >= 8 && hour < 20 ? 'day' : 'night';
  const animation: 'alive' | 'playful' = period === 'day' ? 'alive' : 'playful';
  const start = new Date(year, month, day, period === 'day' ? 8 : 20);
  if (hour < 8) start.setDate(start.getDate() - 1);
  const dateKey = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
  const slotKey = `${start.getFullYear()}-${String(start.getMonth() + 1).padStart(2, '0')}-${String(start.getDate()).padStart(2, '0')}-${period}`;
  return { animation, dateKey, period, slotKey };
}

export function useDailyLogoPreset(enabled = true) {
  const [preset, setPreset] = useState(() => getDailyLogoPreset());

  useEffect(() => {
    if (!enabled) return;
    let timer: ReturnType<typeof setTimeout>;
    function synchronize() {
      clearTimeout(timer);
      const now = new Date();
      const next = getDailyLogoPreset(now);
      setPreset(previous => previous.slotKey === next.slotKey ? previous : next);
      const hour = now.getHours();
      const boundary = new Date(now.getFullYear(), now.getMonth(), now.getDate(), hour < 8 ? 8 : hour < 20 ? 20 : 8);
      if (hour >= 20) boundary.setDate(boundary.getDate() + 1);
      timer = setTimeout(synchronize, boundary.getTime() - now.getTime() + 50);
    }
    function resume() {
      if (document.visibilityState === 'visible') synchronize();
    }
    synchronize();
    document.addEventListener('visibilitychange', resume);
    window.addEventListener('focus', resume);
    return () => {
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', resume);
      window.removeEventListener('focus', resume);
    };
  }, [enabled]);

  return enabled ? getDailyLogoPreset() : preset;
}
