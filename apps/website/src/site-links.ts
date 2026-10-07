/** Replace null with the public URLs as each destination becomes available. */
export const siteLinks: Record<'install' | 'userDocs' | 'devDocs' | 'github', string | null> = {
  install: '/docs/getting-started/',
  userDocs: '/docs/',
  devDocs: '/dev-docs/',
  github: 'https://github.com/molr-ai/molr',
};
