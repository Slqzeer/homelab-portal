# Local interface development

Install the locked frontend dependencies once:

```sh
npm --prefix web ci
```

Start the local fixture-backed preview:

```sh
make dev
```

If `make` is unavailable, run `npm --prefix web run dev` instead. Open
<http://127.0.0.1:4321/>. Astro updates the browser while files under
`web/src/` or `web/public/` change, so a production build is not needed between
interface edits. `/admin` contains example diagnostics.

The preview uses visibly fake `.example` targets and does not connect to
Kubernetes, Keycloak, Vault, or the Tailnet. Fixture data exists only when
Astro is running in development mode; production HTML remains rendered and
authorized by the Go BFF. Run `npm --prefix web test` before committing to
exercise that production-rendered boundary, and `npm --prefix web run test:dev`
to verify the development preview.
