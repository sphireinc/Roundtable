package coordinator

import (
	"context"
	"errors"
	"sync"
	"time"

	"roundtable/internal/db"
)

type Health struct {
	Status string `json:"status"`
}

type TickFunc func(context.Context, string) error

type retryState struct {
	next  time.Time
	delay time.Duration
}

type Service struct {
	store  *db.Store
	tick   TickFunc
	mu     sync.RWMutex
	health Health
	retry  map[string]retryState
	log    func(string)
}

func NewService(store *db.Store, tick TickFunc) *Service {
	return &Service{store: store, tick: tick, health: Health{Status: "healthy-idle"}, retry: make(map[string]retryState)}
}

func (s *Service) Health() Health {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.health
}

func (s *Service) SetLogger(log func(string)) {
	s.mu.Lock()
	s.log = log
	s.mu.Unlock()
}

func (s *Service) logEvent(message string) {
	s.mu.RLock()
	log := s.log
	s.mu.RUnlock()
	if log != nil {
		log(message)
	}
}

func (s *Service) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("coordinator interval must be positive")
	}
	if err := s.cycle(ctx); err != nil && ctx.Err() == nil {
		s.setHealth("degraded")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.cycle(ctx); err != nil && ctx.Err() != nil {
				return nil
			}
		}
	}
}

func (s *Service) cycle(ctx context.Context) error {
	runs, err := s.store.ListRuns(ctx)
	if err != nil {
		s.setHealth("degraded")
		s.logEvent("run discovery failed; retrying")
		return err
	}
	now := time.Now()
	active := 0
	activeIDs := make(map[string]bool)
	for _, run := range runs {
		if ctx.Err() != nil {
			return nil
		}
		if run.Status != "active" {
			continue
		}
		active++
		activeIDs[run.ID] = true
		s.mu.RLock()
		retry, waiting := s.retry[run.ID]
		s.mu.RUnlock()
		if waiting && now.Before(retry.next) {
			continue
		}
		if err := s.tick(ctx, run.ID); err != nil {
			s.mu.Lock()
			if retry.delay == 0 {
				retry.delay = time.Second
			} else {
				retry.delay *= 2
				if retry.delay > 30*time.Second {
					retry.delay = 30 * time.Second
				}
			}
			retry.next = time.Now().Add(retry.delay)
			s.retry[run.ID] = retry
			s.health.Status = "degraded"
			s.mu.Unlock()
			s.logEvent("run tick failed; retry scheduled")
			continue
		}
		s.mu.Lock()
		_, wasRetrying := s.retry[run.ID]
		delete(s.retry, run.ID)
		s.mu.Unlock()
		if wasRetrying {
			s.logEvent("run tick recovered")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for runID := range s.retry {
		if !activeIDs[runID] {
			delete(s.retry, runID)
		}
	}
	if len(s.retry) > 0 {
		s.health.Status = "degraded"
	} else if active == 0 {
		s.health.Status = "healthy-idle"
	} else {
		s.health.Status = "ready"
	}
	return nil
}

func (s *Service) setHealth(status string) {
	s.mu.Lock()
	s.health.Status = status
	s.mu.Unlock()
}
