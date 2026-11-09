import type { Page } from '@playwright/test';

/** Chat panels are complementary regions when docked and dialogs when overlaid. */
export function chatPanel(page: Page, name: string) {
  return page.getByRole('complementary', { name, exact: true })
    .or(page.getByRole('dialog', { name, exact: true }));
}
