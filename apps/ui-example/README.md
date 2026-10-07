# Focus UI development reference

A framework-independent chat implementation using @molr/sdk and plain HTML/TypeScript. It does not depend on React or @molr/ui and contains no credentials or Runtime proxy.

This manifest is a development reference for the earlier isolated Host API and cannot be installed or activated through browser preferences. Product themes are installed by administrators as authorized plugin packages using the separate [Plugin API](../../dev-docs/runtime/PLUGINS.md).

`npm run dev:ui-example` serves the reference assets. `npm run build --workspace @molr/ui-example` builds them. Future embedding must use an authorized host bridge and immutable versioned assets. See [theme architecture](../../dev-docs/frontend/UI_PACKAGES.md).
