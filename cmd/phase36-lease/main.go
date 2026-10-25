package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"bet9ja-vfl/services/ingest"
)

type report struct {
	Phase              string             `json:"phase"`
	InspectedAt        time.Time          `json:"inspected_at"`
	LeaseKey           string             `json:"lease_key"`
	ReclaimRequested   bool               `json:"reclaim_requested"`
	Reclaimed          bool               `json:"reclaimed"`
	ReconcileRequested bool               `json:"reconcile_orphaned_jobs_requested"`
	ReconciledJobs     int                `json:"reconciled_orphaned_jobs"`
	Lease              ingest.LeaseStatus `json:"lease"`
	Message            string             `json:"message"`
}

func main() {
	databaseURL := os.Getenv("PHASE36_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	leaseKey := "live-monitor"
	reclaimExpired := false
	reconcileOrphaned := false
	orphanAge := 4 * time.Minute
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&leaseKey, "lease-key", leaseKey, "scheduler lease key to inspect")
	flag.BoolVar(&reclaimExpired, "reclaim-expired", reclaimExpired, "atomically reclaim only an expired lease")
	flag.BoolVar(&reconcileOrphaned, "reconcile-orphaned-jobs", reconcileOrphaned, "cancel old running jobs only when no valid lease remains")
	flag.DurationVar(&orphanAge, "orphan-age", orphanAge, "minimum age for explicit orphaned-job reconciliation")
	flag.Parse()
	if databaseURL == "" {
		fatal("set PHASE36_DATABASE_URL, DATABASE_URL, or -database-url")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	repo := ingest.NewRepository(db)
	lease, err := repo.GetLease(ctx, leaseKey)
	if err != nil {
		fatal(err.Error())
	}
	result := report{
		Phase:              "3.6",
		InspectedAt:        time.Now().UTC(),
		LeaseKey:           leaseKey,
		ReclaimRequested:   reclaimExpired,
		ReconcileRequested: reconcileOrphaned,
		Lease:              lease,
	}
	if !lease.Exists {
		result.Message = "no lease row exists; no worker ownership is active"
	} else if !lease.Expired {
		result.Message = "lease is valid and was not reclaimed"
	} else {
		result.Message = "lease is expired; use -reclaim-expired only after confirming the worker is not running"
	}
	if reclaimExpired {
		reclaimedLease, reclaimed, reclaimErr := repo.ReclaimExpiredLease(ctx, leaseKey)
		if reclaimErr != nil {
			fatal(reclaimErr.Error())
		}
		result.Reclaimed = reclaimed
		result.Lease = reclaimedLease
		if reclaimed {
			result.Message = fmt.Sprintf("expired lease reclaimed atomically; cancelled %d running job record(s); a new worker may acquire it", reclaimedLease.CancelledJobCount)
		} else if reclaimedLease.Exists && !reclaimedLease.Expired {
			result.Message = "lease became or remains valid; it was not reclaimed"
		} else {
			result.Message = "no expired lease was reclaimed"
		}
	}
	if reconcileOrphaned {
		count, reconcileErr := repo.ReconcileOrphanedRunningJobs(ctx, leaseKey, orphanAge)
		if reconcileErr != nil {
			fatal(reconcileErr.Error())
		}
		result.ReconciledJobs = count
		if count > 0 {
			result.Message = fmt.Sprintf("reconciled %d old running job record(s); no valid lease was present", count)
		} else if lease.Exists && !lease.Expired {
			result.Message = "valid lease remains; no orphaned job records were changed"
		} else if result.Message == "" {
			result.Message = "no eligible orphaned job records were found"
		}
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(encoded))
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "phase36-lease:", message)
	os.Exit(1)
}
