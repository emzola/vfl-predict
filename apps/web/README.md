# Private Bet9ja VFL dashboard

This is the Phase 4 private Next.js App Router frontend. It reads the
authenticated Go API through same-origin /api/v1/* requests and never
connects to PostgreSQL or the Bet9ja provider directly.

## Local development

Run npm install, set API_BASE_URL to the private Go API address, and run npm
run dev.

The Go API must be running separately. Configure AUTHORIZED_EMAIL,
APP_BASE_URL, and the mailer in the API environment. Development defaults to
the in-memory mailer; it keeps the generated link in the API process for
tests and does not print it to logs.

The frontend does not start workers or trigger upstream polling. It only reads
stored records and requests conservative browser refreshes for mutable views.
