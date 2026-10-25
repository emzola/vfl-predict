# Lightweight host monitoring

Copy `bet9ja-vfl-health.service`, `bet9ja-vfl-health.timer`, and
`scripts/production-health-report.sh` to the VPS after reviewing them. Enable
the timer with:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now bet9ja-vfl-health.timer
sudo journalctl -u bet9ja-vfl-health.service
```

The check is read-only. It reports Docker disk usage, container status, memory
usage, PostgreSQL container health, and filesystem free space. It exits with a
warning below 25% free space and a critical status below 15%. Connect the
non-zero systemd result to the operator's existing alert channel; no alert
provider or secret is embedded in the repository.
