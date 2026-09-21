package cron

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/robfig/cron/v3"

	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/reconcile"
)

// Scheduler 提供后端周期性任务的注册与生命周期管理
type Scheduler struct {
	cron          *cron.Cron
	cl            *Cleanup
	rc            *reconcile.Reconciler
	mu            sync.Mutex
	pluginEntries map[string][]cron.EntryID
}

// AddPluginTask 向宿主调度器注册受信可执行插件任务
func (s *Scheduler) AddPluginTask(pluginID, name, schedule string, task func()) error {
	if strings.TrimSpace(schedule) == "" || task == nil {
		return errors.New("plugin task schedule and callback are required")
	}
	entryID, err := s.cron.AddFunc(schedule, task)
	if err != nil {
		return fmt.Errorf("add plugin task %s/%s: %w", pluginID, name, err)
	}
	s.mu.Lock()
	s.pluginEntries[pluginID] = append(s.pluginEntries[pluginID], entryID)
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) RemovePluginTasks(pluginID string) {
	s.mu.Lock()
	entries := s.pluginEntries[pluginID]
	delete(s.pluginEntries, pluginID)
	s.mu.Unlock()
	for _, entryID := range entries {
		s.cron.Remove(entryID)
	}
}

func NewScheduler(db *gorm.DB, cfg *config.Config) *Scheduler {
	logger := log.New(os.Stdout, "[OSS cron] ", log.LstdFlags)
	c := cron.New(cron.WithLogger(cron.PrintfLogger(logger)))
	return &Scheduler{
		cron:          c,
		cl:            NewCleanup(db, cfg),
		rc:            reconcile.New(db, cfg),
		pluginEntries: make(map[string][]cron.EntryID),
	}
}

func (s *Scheduler) Register() {
	spec := "0 3 * * *"
	_, _ = s.cron.AddFunc(spec, func() {
		if err := s.cl.CompactTombstones(); err != nil {
			log.Printf("[OSS cron] CompactTombstones error: %v", err)
		}
	})
	reconcileSpec := fmt.Sprintf("@every %dh", s.cl.Cfg.Sync.EffectiveReconcileIntervalHours())
	_, _ = s.cron.AddFunc(reconcileSpec, func() {
		_, err := s.rc.Run(false)
		if err != nil {
			log.Printf("[OSS cron] storage reconciliation error: %v", err)
			return
		}
	})
	_, _ = s.cron.AddFunc(spec, func() {
		if err := s.cl.PurgeOrphanAttachments(); err != nil {
			log.Printf("[OSS cron] PurgeOrphanAttachments error: %v", err)
		}
	})
	_, _ = s.cron.AddFunc(spec, func() {
		if err := s.cl.PurgeExpiredHistory(); err != nil {
			log.Printf("[OSS cron] PurgeExpiredHistory error: %v", err)
		}
	})
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop(ctx context.Context) error {
	if s.cron == nil {
		return nil
	}
	stopCtx := s.cron.Stop()
	if ctx == nil {
		<-stopCtx.Done()
		return nil
	}
	select {
	case <-stopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) Cleanup() *Cleanup { return s.cl }

func (s *Scheduler) Reconciler() *reconcile.Reconciler { return s.rc }
