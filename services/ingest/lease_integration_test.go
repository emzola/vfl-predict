package ingest

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPhase36LeaseOwnershipAndExpiredRecovery(t *testing.T) {
	databaseURL := os.Getenv("PHASE36_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("PHASE3_DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("set PHASE36_DATABASE_URL or PHASE3_DATABASE_URL to a dedicated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := OpenDatabase(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ApplyMigrations(ctx, db, "../../db/migrations"); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	key := "phase36-lease-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	ownerA := "phase36-owner-a"
	ownerB := "phase36-owner-b"
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM scheduler_leases WHERE lease_key = $1`, key)
	})

	tokenA, acquired, err := repo.AcquireLease(ctx, key, ownerA, time.Minute)
	if err != nil || !acquired || tokenA == "" {
		t.Fatalf("first worker acquire token=%q acquired=%t err=%v", tokenA, acquired, err)
	}
	if _, acquired, err := repo.AcquireLease(ctx, key, ownerB, time.Minute); err != nil || acquired {
		t.Fatalf("second worker acquired a valid lease: acquired=%t err=%v", acquired, err)
	}
	status, err := repo.GetLease(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists || status.Expired || status.OwnerID != ownerA || status.LeaseToken != tokenA {
		t.Fatalf("unexpected valid lease status: %+v", status)
	}
	if _, reclaimed, err := repo.ReclaimExpiredLease(ctx, key); err != nil || reclaimed {
		t.Fatalf("valid lease was reclaimed: reclaimed=%t err=%v", reclaimed, err)
	}
	jobID, err := repo.StartJobExecution(ctx, key, ownerA, 1, `{}`)
	if err != nil || jobID == "" {
		t.Fatalf("start lease-owned job id=%q err=%v", jobID, err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE scheduler_leases SET lease_until = now() - interval '1 second' WHERE lease_key = $1`, key); err != nil {
		t.Fatal(err)
	}
	status, err = repo.GetLease(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Exists || !status.Expired {
		t.Fatalf("expired lease was not visible as expired: %+v", status)
	}
	reclaimedLease, reclaimed, err := repo.ReclaimExpiredLease(ctx, key)
	if err != nil || !reclaimed {
		t.Fatalf("expired lease was not reclaimed: reclaimed=%t err=%v", reclaimed, err)
	}
	if reclaimedLease.OwnerID != ownerA || reclaimedLease.LeaseToken != tokenA || !reclaimedLease.Expired {
		t.Fatalf("reclaimed lease evidence mismatch: %+v", reclaimedLease)
	}
	if reclaimedLease.CancelledJobCount != 1 {
		t.Fatalf("cancelled job count=%d want 1", reclaimedLease.CancelledJobCount)
	}
	var jobStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM job_execution_history WHERE execution_id = $1::uuid`, jobID).Scan(&jobStatus); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "cancelled" {
		t.Fatalf("reclaimed lease job status=%q want cancelled", jobStatus)
	}
	if status, err := repo.GetLease(ctx, key); err != nil {
		t.Fatal(err)
	} else if status.Exists {
		t.Fatalf("reclaimed lease row still exists: %+v", status)
	}

	tokenB, acquired, err := repo.AcquireLease(ctx, key, ownerB, time.Minute)
	if err != nil || !acquired || tokenB == "" {
		t.Fatalf("replacement worker acquire token=%q acquired=%t err=%v", tokenB, acquired, err)
	}
	if err := repo.ReleaseLease(ctx, key, ownerB, tokenB); err != nil {
		t.Fatal(err)
	}
	orphanKey := key + "-orphan"
	orphanID, err := repo.StartJobExecution(ctx, orphanKey, ownerA, 1, `{}`)
	if err != nil || orphanID == "" {
		t.Fatalf("start orphan job id=%q err=%v", orphanID, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE job_execution_history SET started_at = now() - interval '5 minutes' WHERE execution_id = $1::uuid`, orphanID); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.ReconcileOrphanedRunningJobs(ctx, orphanKey, 4*time.Minute); err != nil || count != 1 {
		t.Fatalf("orphan reconciliation count=%d err=%v", count, err)
	}
}
