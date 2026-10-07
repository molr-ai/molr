import { test, expect } from '@playwright/test';

for (const kind of ['wordmark', 'mark']) {
  test(`${kind} logo animations keep the finalized pose and fit their canvas`, async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.goto('/tests/fixtures/');
    const issues = await page.evaluate(async kind => {
      const issues: string[] = [];
      const motions = ['blink', 'idle', 'curious', 'sleepy', 'greet', 'loading', 'look', 'listen', 'stretch', 'nuzzle', 'earflick', 'peek', 'alive', 'playful'];
      async function load(suffix: string) {
        const source = await fetch(`/brand/molr-${kind}${suffix}.svg`).then(r => r.text());
        const host = document.createElement('div');
        host.innerHTML = source;
        const svg = host.firstElementChild as unknown as SVGSVGElement;
        svg.style.width = `${svg.viewBox.baseVal.width}px`;
        svg.style.height = `${svg.viewBox.baseVal.height}px`;
        document.body.append(host);
        svg.pauseAnimations();
        return { svg, host };
      }
      async function seek(svg: SVGSVGElement, time: number) {
        svg.setCurrentTime(time);
        await new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      }
      function metrics(svg: SVGSVGElement) {
        const artwork = svg.querySelector('[data-motion-art], [data-idle-motion]') ?? svg;
        const toRoot = svg.getScreenCTM()!.inverse();
        return [...artwork.querySelectorAll<SVGGraphicsElement>('[data-letter], [data-eye]')].map(el => {
          const matrix = toRoot.multiply(el.getScreenCTM()!);
          const key = el.getAttribute('data-letter') ?? `eye-${el.getAttribute('data-eye')}`;
          let points: DOMPoint[];
          if (el.hasAttribute('data-letter')) {
            const path = el as SVGPathElement;
            points = [0, .25, .5, .75, 1].map(t => {
              const pt = path.getPointAtLength(path.getTotalLength() * t);
              return new DOMPoint(pt.x, pt.y).matrixTransform(matrix);
            });
          } else {
            const box = el.getBBox();
            points = [new DOMPoint(box.x, box.y), new DOMPoint(box.x + box.width, box.y + box.height)].map(p => p.matrixTransform(matrix));
          }
          const box = el.getBBox();
          const padding = el.hasAttribute('data-letter') ? Number(getComputedStyle(el).strokeWidth.replace('px', '')) / 2 : 0;
          const corners = [[box.x-padding, box.y-padding], [box.x+box.width+padding, box.y-padding], [box.x-padding, box.y+box.height+padding], [box.x+box.width+padding, box.y+box.height+padding]].map(([x,y]) => new DOMPoint(x,y).matrixTransform(matrix));
          return { key, points: points.flatMap(p => [p.x, p.y]), corners };
        });
      }
      const still = await load('');
      const expected = metrics(still.svg);
      still.host.remove();
      for (const motion of motions) {
        const {svg, host} = await load(`-${motion}`);
        const durations = [...svg.querySelectorAll('animate, animateTransform')].map(el => parseFloat(el.getAttribute('dur')!));
        const duration = durations.length ? Math.max(...durations) : 4;
        for (const time of [0, duration * .25, duration * .5, duration * .75, duration]) {
          await seek(svg, time);
          const measured = metrics(svg);
          for (const {key, points, corners} of measured) {
            const vb = svg.viewBox.baseVal;
            if (corners.some(p => p.x < -.05 || p.y < -.05 || p.x > vb.width + .05 || p.y > vb.height + .05)) issues.push(`${motion}@${time}: ${key} clips`);
            if (time === 0 || time === duration) {
              const reference = expected.find(x => x.key === key)!;
              if (!reference || points.some((v, i) => Math.abs(v - reference.points[i]) > .025)) issues.push(`${motion}@${time}: ${key} does not return to the static pose`);
            }
          }
        }
        host.remove();
      }
      return issues;
    }, kind);
    expect(issues).toEqual([]);
  });
}
