// Run under the external deadline in test-desktop-package.mjs.
// Keep native startup errors in the supervisor's failure log, including errors
// emitted before electron.launch() returns.
process.env.DEBUG = [process.env.DEBUG, 'pw:browser'].filter(Boolean).join(',');
const { testDesktopPackage } = await import('./test-desktop-package.mjs');

await testDesktopPackage();
