package serverplugin

import (
	"context"
	"fmt"
)

type pluginTaskScheduler interface {
	AddPluginTask(string, string, string, func()) error
	RemovePluginTasks(string)
}

func (m *Manager) RegisterTasks(scheduler pluginTaskScheduler) error {
	m.mu.Lock()
	m.scheduler = scheduler
	registrations := make(map[string]ExtensionRegistration, len(m.registrations))
	for pluginID, registration := range m.registrations {
		registrations[pluginID] = registration
	}
	m.mu.Unlock()
	for pluginID, registration := range registrations {
		if err := m.registerPluginTasks(pluginID, registration); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) registerPluginTasks(pluginID string, registration ExtensionRegistration) error {
	m.mu.RLock()
	scheduler := m.scheduler
	m.mu.RUnlock()
	if scheduler == nil {
		return nil
	}
	scheduler.RemovePluginTasks(pluginID)
	for _, task := range registration.Tasks {
		callback := task.Callback
		if callback == "" {
			callback = task.Name
		}
		currentTask := task
		if err := scheduler.AddPluginTask(pluginID, task.Name, task.Schedule, func() {
			_, _ = m.invokeCallback(context.Background(), pluginID, callback, PluginRequest{
				Method: "TASK",
				Path:   "/tasks/" + currentTask.Name,
			})
		}); err != nil {
			return fmt.Errorf("register plugin task %s/%s: %w", pluginID, task.Name, err)
		}
	}
	return nil
}
