import { useState } from 'react';
import { ArrowDownToLine } from 'lucide-react';
import { appIconSizes, SegmentedControl } from '@molr/ui';
import type { Messages } from './messages';

export function AppIconShowcase({ copy, svgAssetBase }: { copy: Messages['appIcon']; svgAssetBase: string }) {
  const [shape, setShape] = useState<'rounded' | 'square'>('rounded');
  const name = `molr-icon${shape === 'square' ? '-square' : ''}`;
  const asset = `${import.meta.env.BASE_URL}brand/${name}`;
  const svgAsset = `${svgAssetBase}/${name}.svg`;
  const shapeLabel = copy[shape];
  return (
    <section className="foundation-section" id="app-icon" aria-labelledby="app-icon-title">
      <div className="section-heading">
        <div><span className="section-number">03 / IDENTITY</span><h2 id="app-icon-title">{copy.title}</h2></div>
        <p>{copy.description}<br />{copy.descriptionEnd}</p>
      </div>
      <SegmentedControl label={copy.shape} hideLabel className="app-icon-controls" value={shape} onValueChange={setShape} options={[{ value: 'rounded', label: copy.rounded }, { value: 'square', label: copy.square }]} />
      <div className="app-icon-showcase">
        <div className="app-icon-preview">
          <span className="app-icon-preview__label">MOLR / APP ICON</span>
          <img className="app-icon-preview__hero" src={svgAsset} width="160" height="160" alt={copy.alt} />
          <div className="app-icon-preview__sizes" role="group" aria-label={copy.preview}>
            {[16, 32, 48].map(size => (
              <figure key={size}>
                <div><img src={`${asset}-${size}.png`} width={size} height={size} alt="" /></div>
                <figcaption>{size} px</figcaption>
              </figure>
            ))}
          </div>
        </div>
        <div className="app-icon-downloads">
          <div className="app-icon-downloads__heading">
            <div><h3 id="app-icon-downloads-title">{copy.downloads}</h3><p>{shape === 'square' ? copy.squareNote : copy.roundedNote}</p></div>
            <a className="app-icon-svg" href={svgAsset} download aria-label={copy.svgDownload(shapeLabel)}>{copy.svg}<ArrowDownToLine size={14} aria-hidden="true" /></a>
          </div>
          <ul className="app-icon-downloads__list" aria-labelledby="app-icon-downloads-title">
            {appIconSizes.map(size => (
              <li key={size}>
                <a href={`${asset}-${size}.png`} download aria-label={copy.download(size, shapeLabel)}>
                  <span><strong>{size} × {size}</strong><small>PNG · px</small></span>
                  <ArrowDownToLine size={16} aria-hidden="true" />
                </a>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}
