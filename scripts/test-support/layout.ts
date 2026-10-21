import type { ElementHandle, Locator } from '@playwright/test';

type LayoutRect = { x: number; y: number; width: number; height: number };

// For elements in the same frame. Read related rectangles in one renderer task:
// separate boundingBox calls can observe different scroll or layout positions.
export async function readLayout<T extends [Locator, ...Locator[]]>(...locators: T): Promise<{ [K in keyof T]: LayoutRect }> {
  const handles: ElementHandle<HTMLElement | SVGElement>[] = [];
  try {
    for (const locator of locators) {
      const handle = await locator.elementHandle();
      if (!handle) throw new Error(`Layout element not found: ${locator}`);
      handles.push(handle);
    }
    return await handles[0].evaluate((_, elements) => elements.map(element => {
      if (!element.isConnected || !element.getClientRects().length) throw new Error('Layout element is detached or hidden');
      const { x, y, width, height } = element.getBoundingClientRect();
      return { x, y, width, height };
    }), handles) as { [K in keyof T]: LayoutRect };
  } finally {
    await Promise.all(handles.map(handle => handle.dispose()));
  }
}
