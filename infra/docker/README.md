# Phase 0 development environment

`docker-compose.yml` starts a local PostgreSQL 16 instance on `127.0.0.1:5432`. It has no provider network path and no application containers yet. This is intentional: live provider connectivity must wait for authorized source-contract validation.

Set `POSTGRES_PASSWORD` in an untracked `.env` file or the current shell before starting the service. The repository contains only placeholders.
