import type { GPUDevice } from '../../../sandbox-types';
import { gpuName } from '../../../gpu';
import { useCopy } from '../../../state';

export function GPUDeviceList({ ids, devices }: { ids: readonly string[]; devices: readonly GPUDevice[] }) {
  const { locale } = useCopy();
  return <ul className="gpu-device-list">{ids.map(id => <li key={id} title={id}>
    <span>{gpuName(id, devices, locale)}</span>
    {!devices.find(device => device.ID === id)?.Available && <small>{locale === 'zh' ? '设备不可用' : 'Device unavailable'}</small>}
  </li>)}</ul>;
}

/** A full-width hardware section, separate from the numeric quota grid. */
export function GPUAccessSummary({ ids = [], devices }: { ids?: readonly string[]; devices?: readonly GPUDevice[] | null }) {
  const { locale } = useCopy();
  if (!devices?.length) return null;
  return <div className="gpu-access-summary" role="group" aria-label={locale === 'zh' ? 'GPU 授权' : 'GPU access'}>
    <p className="gpu-access-summary__label">GPU</p>
    {ids.length ? <GPUDeviceList ids={ids} devices={devices} /> : <p>{locale === 'zh' ? '未授权' : 'Not granted'}</p>}
  </div>;
}
