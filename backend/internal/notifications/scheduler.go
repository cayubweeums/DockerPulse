package notifications

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dockpulse/dockmgr/internal/database"
	"github.com/dockpulse/dockmgr/internal/driver"
	"github.com/dockpulse/dockmgr/internal/updater"
)

type DriverResolverFunc func(ctx context.Context, host *database.Host) (driver.HostDriver, error)

type Scheduler struct {
	db          *database.DB
	dispatcher  *Dispatcher
	updater     *updater.UpdateChecker
	getDriver   DriverResolverFunc
	stopChan    chan struct{}
	mu          sync.Mutex
	running     bool
	triggerChan chan struct{}
}

func NewScheduler(db *database.DB, dispatcher *Dispatcher, updater *updater.UpdateChecker, getDriver DriverResolverFunc) *Scheduler {
	return &Scheduler{
		db:          db,
		dispatcher:  dispatcher,
		updater:     updater,
		getDriver:   getDriver,
		stopChan:    make(chan struct{}),
		triggerChan: make(chan struct{}, 1),
	}
}

// Start begins the background update check loop
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go s.loop()
	log.Println("[Scheduler] Update check scheduler started")
}

// Stop shuts down the scheduler loop
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopChan)
	log.Println("[Scheduler] Update check scheduler stopped")
}

// TriggerNow requests an immediate check run outside the scheduled interval
func (s *Scheduler) TriggerNow() {
	select {
	case s.triggerChan <- struct{}{}:
	default:
	}
}

func (s *Scheduler) loop() {
	for {
		cfg, err := s.db.GetSchedulerConfig()
		intervalMin := 360
		if err == nil && cfg.IntervalMinutes > 0 {
			intervalMin = cfg.IntervalMinutes
		}
		interval := time.Duration(intervalMin) * time.Minute

		select {
		case <-s.stopChan:
			return
		case <-s.triggerChan:
			s.runCheck()
		case <-time.After(interval):
			s.runCheck()
		}
	}
}

func (s *Scheduler) RunCheckSync(ctx context.Context) error {
	return s.executeCheck(ctx)
}

func (s *Scheduler) runCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.executeCheck(ctx); err != nil {
		log.Printf("[Scheduler] Check execution error: %v", err)
	}
}

func (s *Scheduler) executeCheck(ctx context.Context) error {
	cfg, err := s.db.GetSchedulerConfig()
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}

	cfg.LastRun = time.Now().UTC()
	cfg.NextRun = cfg.LastRun.Add(time.Duration(cfg.IntervalMinutes) * time.Minute)
	_ = s.db.SaveSchedulerConfig(cfg)

	hosts, err := s.db.ListHosts()
	if err != nil {
		return err
	}

	for _, h := range hosts {
		if h.Status == "offline" || h.Status == "unreachable" {
			continue
		}

		d, err := s.getDriver(ctx, &h)
		if err != nil {
			log.Printf("[Scheduler] Failed to get driver for host %s: %v", h.Name, err)
			continue
		}

		results, err := s.updater.CheckHostContainers(ctx, d, h.ID)
		if err != nil {
			log.Printf("[Scheduler] Error checking updates for host %s: %v", h.Name, err)
			continue
		}

		var availableUpdates []string
		for _, res := range results {
			if res.HasUpdate {
				availableUpdates = append(availableUpdates, res.Image)
			}
		}

		if len(availableUpdates) > 0 {
			title := fmt.Sprintf("Updates Available on %s", h.Name)
			msg := fmt.Sprintf("%d container image update(s) detected: %s", len(availableUpdates), strings.Join(availableUpdates, ", "))

			// Record in-app notification
			notif := &database.Notification{
				Title:     title,
				Message:   msg,
				Type:      "update",
				HostID:    h.ID,
				Read:      false,
				CreatedAt: time.Now().UTC(),
			}
			if err := s.db.CreateNotification(notif); err != nil {
				log.Printf("[Scheduler] Failed to persist in-app notification: %v", err)
			}

			// Dispatch external notifications (ntfy, Discord, Signal)
			alert := AlertPayload{
				Title:   title,
				Message: msg,
				Type:    "update",
				Host:    h.Name,
			}
			if err := s.dispatcher.Dispatch(ctx, alert); err != nil {
				log.Printf("[Scheduler] External dispatch warning: %v", err)
			}
		}
	}

	return nil
}
