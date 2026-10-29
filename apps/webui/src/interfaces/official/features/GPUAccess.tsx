import type { GPUDevice } from '@molr/sdk';
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
export function GPUAccessSummary({ ids = [], devices, gpuAccessMode }: { gpuAccessMode?: string; ids?: readonly string[]; devices?: readonly GPUDevice[] | null }) {
  const { locale } = useCopy();
  if (gpuAccessMode === 'shared') return <div className="gpu-access-summary" role="group" aria-label={locale === 'zh' ? 'GPU 使用范围' : 'GPU access'}><p className="gpu-access-summary__label">GPU</p><p>{locale === 'zh' ? '计算权限包含系统允许访问的全部 GPU；共享使用，不单独选卡或限制显存。' : 'Compute permission includes all GPUs accessible under system permissions; access is shared, without device selection or GPU memory limits.'}</p></div>;
  if (!devices?.length) return null;
  return <div className="gpu-access-summary" role="group" aria-label={locale === 'zh' ? 'GPU 授权' : 'GPU access'}>
    <p className="gpu-access-summary__label">GPU</p>
    {ids.length ? <GPUDeviceList ids={ids} devices={devices} /> : <p>{locale === 'zh' ? '未授权' : 'Not granted'}</p>}
  </div>;
}
