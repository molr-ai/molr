import { Button } from '@molr/ui';
import { useCopy, useQuery } from '../../../state';

import type { DiscoveredProvider as LocalProvider } from '../../../api/types';
export function LocalProviderDiscovery({ disabled, onSelect }: { disabled: boolean; onSelect: (item: LocalProvider) => void }) {
 const { locale } = useCopy(), zh = locale === 'zh';
 const query = useQuery<LocalProvider[]>('/api/resources/discover-local');
 return <section className="local-discovery" aria-label={zh ? '发现本机模型' : 'Discover local models'}>
  <div className="setup-actions"><strong>{zh ? 'Molr 所在设备上的模型服务' : 'Model services on the Molr device'}</strong><Button variant="ghost" size="sm" loading={query.loading} disabled={disabled} onClick={query.reload}>{zh ? '重新检测' : 'Scan again'}</Button></div>
  {query.loading ? <p className="muted" role="status">{zh ? '正在检测，可同时配置其他提供商…' : 'Scanning; you can configure another provider below…'}</p> : query.error ? <p className="muted">{zh ? '检测暂不可用，可手动填写地址。' : 'Scan unavailable. Enter an address manually.'}</p> : !query.data?.length ? <p className="muted">{zh ? '未发现本机服务，可连接其他提供商或手动填写地址。' : 'No local services found. Connect another provider or enter an address.'}</p> : query.data.map(item => <div className="local-provider-result" key={item.Endpoint}>
   <div><strong>{item.State === 'authentication_required' ? (zh ? '本机服务（待验证）' : 'Local service (unverified)') : item.Name === 'OpenAI compatible' ? (zh ? 'OpenAI 兼容服务' : item.Name) : item.Name === 'Ollama compatible' ? (zh ? 'Ollama 兼容服务' : item.Name) : item.Name}</strong><p className="muted">{item.Endpoint}</p><small>{item.State === 'authentication_required' ? (zh ? '需要 API Key' : 'API key required') : item.State === 'empty' ? (zh ? '暂无可用模型' : 'No models available') : (zh ? `${item.Models.length} 个可用模型` : `${item.Models.length} models`)}</small></div>
   <Button variant="secondary" disabled={disabled || !!item.ResourceID} onClick={() => onSelect(item)}>{item.ResourceID ? (zh ? '已连接' : 'Connected') : item.State === 'authentication_required' ? (zh ? '配置' : 'Configure') : (zh ? '使用' : 'Use')}</Button>
  </div>)}
 </section>;
}
