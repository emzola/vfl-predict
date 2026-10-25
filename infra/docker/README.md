# PostgreSQL development environment

docker-compose.yml starts a local PostgreSQL 16 instance on
127.0.0.1:5432 by default, plus private-network API and Next.js containers.
The web container is the only local host entry point (127.0.0.1:3000 by
default); PostgreSQL and the Go API are not published to the host.

Set POSTGRES_PASSWORD in an untracked .env file or the current shell before starting the service. The repository contains only placeholders.
Set INTERNAL_API_TOKEN in the same untracked environment because the API
container binds to its private Docker network rather than loopback.
Set AUTHORIZED_EMAIL, APP_BASE_URL, and a production mailer before using
magic-link authentication. The default development mailer keeps links in
memory and does not log them.
The PostgreSQL host port is configurable with `POSTGRES_HOST_PORT` and
defaults to `5432`. Use an alternate local port when another service owns the
default, and update DATABASE_URL accordingly. Keep the password in an
untracked .env file or process environment.

Workers remain independent commands and are not started by the frontend or
dashboard. Run the live monitor, prediction worker, and settlement process
separately with their existing operator safeguards.

`Caddyfile.example` is a reference TLS reverse-proxy configuration. Replace
the example hostname, terminate HTTPS at the reverse proxy, and forward only
to the private `web` service. The example is not a production deployment and
must be combined with firewall rules, secret management, logging, and database
backup/restore procedures.
