package serverplugin

import (
	"context"
	"fmt"
)

// pluginTaskScheduler 是插件定时任务在宿主调度器上的注册入口
type pluginTaskScheduler interface {
	AddPluginTask(string, string, string, func()) error
	RemovePluginTasks(string)
}

// RegisterTasks 注入调度器并对已注册插件补齐定时任务
// 注入后再次替换调度器会触发全量重新注册
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

// registerPluginTasks 把单个插件的定时任务写入调度器
// 未注入调度器时静默跳过；任务以 TASK 方法调用，callback 缺省为任务名
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
