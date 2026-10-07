import { useState } from 'react';
import { useUILocale } from '../locale';
import { Dialog } from './Dialog';
import { UIIcon } from './icons';

export type GalleryImage = { id?: string; src: string; alt: string; downloadUrl?: string };
export type ImageGalleryProps = {
  images: readonly GalleryImage[];
  onRemove?: (id: string) => void;
  disabled?: boolean;
  className?: string;
};

function GalleryPicture({ image, full = false }: { image: GalleryImage; full?: boolean }) {
  const { messages } = useUILocale();
  const [failed, setFailed] = useState(false);
  return failed
    ? <span className="ui-image-gallery__error" role="img" aria-label={image.alt}><UIIcon name="warning" />{messages.chat.imageUnavailable}</span>
    : <img src={image.src} alt={image.alt} loading={full ? 'eager' : 'lazy'} decoding="async" onError={() => setFailed(true)} />;
}

export function ImageGallery({ images, onRemove, disabled, className = '' }: ImageGalleryProps) {
  const { messages } = useUILocale();
  const [selected, setSelected] = useState<string | null>(null);
  const key = (image: GalleryImage, position: number) => image.id ?? `${image.src}-${position}`;
  const index = images.findIndex((image, position) => key(image, position) === selected);
  const current = images[index];
  function move(step: number) { const next = (index + step + images.length) % images.length; setSelected(key(images[next], next)); }
  if (!images.length) return null;
  return <div className={`ui-image-gallery ${className}`} onKeyDown={event => {
    if (!current || !['ArrowLeft', 'ArrowRight'].includes(event.key) || event.altKey || event.ctrlKey || event.metaKey) return;
    event.preventDefault(); event.stopPropagation(); move(event.key === 'ArrowLeft' ? -1 : 1);
  }}>
    <ul className="ui-image-gallery__grid" aria-label={messages.chat.images}>
      {images.map((image, position) => <li key={`${key(image, position)}-${position}`}>
        <button type="button" className="ui-image-gallery__thumbnail" aria-label={messages.chat.previewImage(image.alt)} onClick={() => setSelected(key(image, position))}>
          <GalleryPicture key={image.src} image={image} />
        </button>
        {onRemove && image.id && <button type="button" className="ui-image-gallery__remove" disabled={disabled} aria-label={messages.chat.removeAttachment(image.alt)} onClick={() => onRemove(image.id!)}><UIIcon name="close" /></button>}
      </li>)}
    </ul>
    <Dialog open={!!current} onOpenChange={open => { if (!open) setSelected(null); }} title={messages.chat.imagePreview} size="lg" className="ui-image-gallery__dialog">
      {current && <>
        <div className="ui-image-gallery__stage"><GalleryPicture key={current.src} image={current} full /></div>
        <div className="ui-image-gallery__caption">
          <span title={current.alt}>{current.alt}</span>
          {current.downloadUrl && <a className="ui-image-gallery__download" href={current.downloadUrl} download={current.alt}>{messages.chat.downloadImage}</a>}
          <div className="ui-image-gallery__navigation">
            {images.length > 1 && <button type="button" className="ui-icon-button" aria-label={messages.chat.previousImage} onClick={() => move(-1)}><UIIcon name="chevron-left" /></button>}
            <small role="status" aria-live="polite">{index + 1} / {images.length}</small>
            {images.length > 1 && <button type="button" className="ui-icon-button" aria-label={messages.chat.nextImage} onClick={() => move(1)}><UIIcon name="chevron-right" /></button>}
          </div>
        </div>
        {images.length > 1 && <div className="ui-image-gallery__strip" role="group" aria-label={messages.chat.images}>
          {images.map((image, position) => <button key={`${key(image, position)}-${position}`} type="button" aria-label={messages.chat.previewImage(image.alt)} aria-pressed={position === index} onClick={() => setSelected(key(image, position))}><GalleryPicture key={image.src} image={image} /></button>)}
        </div>}
      </>}
    </Dialog>
  </div>;
}
