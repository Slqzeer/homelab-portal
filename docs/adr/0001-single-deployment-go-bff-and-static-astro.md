# Single Go BFF deployment with static Astro assets

The portal uses one repository and one deployable image: Astro, Tailwind CSS, and native JavaScript build static assets that are embedded in a Go BFF. This keeps UI and authorization-contract changes atomic, avoids a Node runtime and separate public API in production, and remains substantially more sober than independently deployed front-end and back-end services.

## Considered Options

Separate front-end and Go repositories were rejected for V1 because they require API compatibility versioning, release ordering, and either two production deployments or artifact transfer into the Go build. Node SSR was rejected because static Astro output satisfies the UI needs without a second runtime.
