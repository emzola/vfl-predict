# PostgreSQL development environment

`docker-compose.yml` starts a local PostgreSQL 16 instance on
`127.0.0.1:5432` by default. It has no always-on application container; the
Phase 1 importer is an explicit operator command and the later API/worker
services remain out of scope.

Set `POSTGRES_PASSWORD` in an untracked `.env` file or the current shell before starting the service. The repository contains only placeholders.
The PostgreSQL host port is configurable with `POSTGRES_HOST_PORT` and
defaults to `5432`. Use an alternate local port when another service owns the
default, and update `DATABASE_URL` accordingly. Keep the password in an
untracked `.env` file or process environment.
