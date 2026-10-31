package ingest

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PriorityProviderRequestScheduler serializes provider requests within a
// process while allowing priority callers to reserve the next available
// request slot ahead of ordinary ingestion work. The database-backed budget
// remains the final cross-process rate limit.
type PriorityProviderRequestScheduler struct {
	repo      *Repository
	budgetKey string
	spacing   time.Duration

	mu              sync.Mutex
	active          bool
	priorityWaiters int
	wake            chan struct{}
}

func NewPriorityProviderRequestScheduler(repo *Repository, budgetKey string, spacing time.Duration) *PriorityProviderRequestScheduler {
	if budgetKey == "" {
		budgetKey = "bet9ja-global"
	}
	return &PriorityProviderRequestScheduler{
		repo:      repo,
		budgetKey: budgetKey,
		spacing:   spacing,
		wake:      make(chan struct{}),
	}
}

// Attach replaces the client's local budget with this scheduler. Priority
// clients are allowed to proceed whenever the current request completes;
// ordinary clients wait while a priority request is queued.
func (s *PriorityProviderRequestScheduler) Attach(client *ProviderClient, priority bool) {
	if s == nil || client == nil {
		return
	}
	client.Budget = NewRequestBudget(0)
	client.RequestGate = func(ctx context.Context) error {
		return s.wait(ctx, priority)
	}
}

func (s *PriorityProviderRequestScheduler) wait(ctx context.Context, priority bool) error {
	if s == nil || s.repo == nil {
		return errors.New("priority provider request scheduler is not initialized")
	}
	if s.spacing < 0 {
		return errors.New("provider request spacing must not be negative")
	}

	registered := false
	if priority {
		s.mu.Lock()
		s.priorityWaiters++
		registered = true
		s.mu.Unlock()
		defer func() {
			if !registered {
				return
			}
			s.mu.Lock()
			s.priorityWaiters--
			s.signalLocked()
			s.mu.Unlock()
		}()
	}

	for {
		s.mu.Lock()
		if !s.active && (priority || s.priorityWaiters == 0) {
			s.active = true
			if priority {
				s.priorityWaiters--
				registered = false
			}
			s.mu.Unlock()

			err := s.repo.WaitForProviderRequest(ctx, s.budgetKey, s.spacing)

			s.mu.Lock()
			s.active = false
			s.signalLocked()
			s.mu.Unlock()
			return err
		}
		wake := s.wake
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		}
	}
}

func (s *PriorityProviderRequestScheduler) signalLocked() {
	close(s.wake)
	s.wake = make(chan struct{})
}
