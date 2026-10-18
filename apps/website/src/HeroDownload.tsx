import { useEffect, useRef, useState } from 'react';
import { Button, Popover, UIIcon } from '@molr/ui';
import { localizedDocPath } from './doc-locale';
import { siteLinks } from './site-links';
import type { Locale } from './site-content';
import type { DesktopPlatform } from './desktop-releases';
import { browserDesktopTarget, desktopOSLabel, desktopArchLabel, preferredDesktopPlatform, type DesktopTarget } from './desktop-target';
import { useDesktopRelease } from './use-desktop-release';
import { Icon } from './HomeIcon';
import './hero-download.css';

export function HeroDownload({ locale }: { locale: Locale }) {
  const zh = locale === 'zh';
  const source = new URL(import.meta.env.VITE_RELEASE_SOURCE || '/releases', window.location.origin).href.replace(/\/$/, '');
  const { release, loading, error, retry } = useDesktopRelease(source);
  const [device, setDevice] = useState<DesktopTarget>({});
  const [selection, setSelection] = useState<DesktopTarget>();
  const [open, setOpen] = useState(false);
  const more = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    let cancelled = false;
    browserDesktopTarget(navigator).then(target => { if (!cancelled) setDevice(target); });
    return () => { cancelled = true; };
  }, []);
  const platforms = release?.platforms || [];
  const platform = preferredDesktopPlatform(platforms, selection || device);
  const available = !loading && !error && platform && release;
  const platformLabel = (p: DesktopPlatform) => `${desktopOSLabel(p.os)} · ${desktopArchLabel(p)}`;
  const label = platform ? platformLabel(platform) : '';
  const optionsTitle = zh ? '选择下载版本' : 'Choose a download';
  const status = loading ? (zh ? '正在读取下载信息…' : 'Loading downloads…')
    : error ? (zh ? '暂时无法读取下载信息。' : 'Downloads are temporarily unavailable.')
    : !platform ? (zh ? '即将推出' : 'Coming soon')
    : `v${release!.version} · ${desktopArchLabel(platform)} · ${platform.file.split('.').at(-1)!.toUpperCase()}`;
  function choose(p: DesktopPlatform) {
    setSelection({ os: p.os, arch: p.arch }); setOpen(false); more.current?.focus();
  }
  return <div className="home-hero-install home-hero-download">
    <div className="home-download-actions">
      {available ? <div className="home-download-split">
        <a className="button button--primary button--lg home-download-primary" href={platform.url} aria-label={`${zh ? '下载' : 'Download'} ${label}`}><Icon name="download" />{label}</a>
        <Popover title={optionsTitle} align="end" width={360} maxHeight={480} open={open} onOpenChange={setOpen}
          trigger={<Button ref={more} size="lg" className="home-download-more" aria-label={zh ? '更多下载选项' : 'More download options'}><UIIcon name="chevron-down" /></Button>}>
          <div className="home-download-options">
            <p className="home-download-version">v{release.version}</p>
            <div role="group" aria-label={zh ? '系统与处理器' : 'Operating system and processor'}>
              {platforms.map(p => <button type="button" key={`${p.os}-${p.arch}`} aria-pressed={platform.os === p.os && platform.arch === p.arch} onClick={() => choose(p)}>
                <span>{platformLabel(p)}</span>
                {platform.os === p.os && platform.arch === p.arch && <Icon name="check" />}
              </button>)}
            </div>
          </div>
        </Popover>
      </div> : !loading && !error ? <div role="status"><Button variant="secondary" size="lg" className="home-download-pending" disabled><Icon name="download" />{status}</Button></div>
        : <p className="home-download-status" role="status">{status}{error && <Button variant="ghost" size="sm" onClick={retry}>{zh ? '重试' : 'Retry'}</Button>}</p>}
      <a className="button button--secondary button--lg" href={localizedDocPath(siteLinks.userDocs!, locale)}><Icon name="book" />{zh ? '查看文档' : 'View docs'}</a>
    </div>
    {available && <p className="home-download-meta" role="status">{status}</p>}
    {available && <p className="home-download-edition">{platform.os === 'win32'
      ? (zh ? '桌面客户端 · 连接已有 Molr 服务' : 'Desktop client · connect to an existing Molr server')
      : (zh ? '桌面应用 · 本机运行或连接远程服务' : 'Desktop app · run locally or connect remotely')}</p>}
    {available && !platform.signed && platform.os !== 'linux' && <p className="home-download-signing">{platform.os === 'darwin'
      ? (zh ? '未公证测试包，macOS 可能阻止首次打开。' : 'Unnotarized test build; macOS may block the first launch.')
      : (zh ? '未签名测试包，Windows 可能提示未知发布者。' : 'Unsigned test build; Windows may show an unknown publisher warning.')}</p>}
    <a className="home-download-alternatives" href="#install">{zh ? '更多安装方式' : 'More installation methods'}<Icon name="down" /></a>
  </div>;
}
