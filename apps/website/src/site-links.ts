/** Replace null with the public URLs as each destination becomes available. */
export const siteLinks: Record<'install' | 'userDocs' | 'adminDocs' | 'devDocs' | 'github', string | null> = {
  install: '/docs/admin/getting-started/',
  userDocs: '/docs/user/',
  adminDocs: '/docs/admin/',
  devDocs: '/docs/dev/',
  github: 'https://github.com/molr-ai/molr',
};
