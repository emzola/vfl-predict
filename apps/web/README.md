# Private Bet9ja VFL dashboard

This is the Phase 4 private Next.js App Router frontend. It reads the
authenticated Go API through same-origin /api/v1/* requests and never
connects to PostgreSQL or the Bet9ja provider directly.

## Local development

Use the committed lockfile for reproducible installation:

```powershell
npm ci --no-audit --no-fund
npm test
npm run typecheck
npm run openapi:validate
npm run build
npm run test:e2e
```

If the workstation or CI environment uses an inspected/corporate registry
certificate, provide its trusted CA explicitly with npm's `cafile` option:

```powershell
npm config get strict-ssl   # must remain true
npm ci --no-audit --no-fund --cafile C:\secure\managed-root.pem
```

Never disable TLS verification. The CA file must remain outside the repository.

For development, set `API_BASE_URL` to the private Go API address and run
`npm run dev`.

The Go API must be running separately. Configure AUTHORIZED_EMAIL,
APP_BASE_URL, and the mailer in the API environment. Development defaults to
the in-memory mailer; it keeps the generated link in the API process for
tests and does not print it to logs.

The frontend does not start workers or trigger upstream polling. It only reads
stored records and requests conservative browser refreshes for mutable views.

`npm run test:e2e` runs deterministic Playwright login/dashboard fixtures. It
requires the pinned Chromium browser (`npx playwright install chromium`) and
does not send email or contact the provider.
