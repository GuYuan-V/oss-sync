package serverplugin

import (
	"sort"
	"strings"
)

// registeredRoute 是命中了插件动态路由的已注册路由
type registeredRoute struct {
	PluginID string
	Route    RegisteredRoute
	Params   map[string]string
}

// registeredMiddleware 是命中了请求路径的已注册中间件
type registeredMiddleware struct {
	PluginID   string
	Middleware RegisteredMiddleware
}

// setRegistration 写入或覆盖插件的运行时注册信息
// 调用方须持有 m.mu 写锁
func (m *Manager) setRegistration(pluginID string, registration ExtensionRegistration) {
	m.registrations[pluginID] = registration
}

// removeRegistration 删除插件的运行时注册信息
// 调用方须持有 m.mu 写锁
func (m *Manager) removeRegistration(pluginID string) {
	delete(m.registrations, pluginID)
}

// Registrations 返回全部注册信息的副本
func (m *Manager) Registrations() map[string]ExtensionRegistration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]ExtensionRegistration, len(m.registrations))
	for pluginID, registration := range m.registrations {
		result[pluginID] = registration
	}
	return result
}

// RegistrationFor 查询单个插件的注册信息
func (m *Manager) RegistrationFor(pluginID string) (ExtensionRegistration, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	registration, ok := m.registrations[pluginID]
	return registration, ok
}

// matchingHooks 返回注册了指定钩子且实例已加载的插件
// 按钩子优先级升序，同优先级按插件 ID 稳定排序
func (m *Manager) matchingHooks(name string) []pluginRegistration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	registrations := make([]pluginRegistration, 0)
	for pluginID, registration := range m.registrations {
		for _, hook := range registration.Hooks {
			if hook.Name == name && m.modules[pluginID] != nil {
				registrations = append(registrations, pluginRegistration{PluginID: pluginID, Value: registration})
				break
			}
		}
	}
	sort.SliceStable(registrations, func(left, right int) bool {
		leftPriority := hookPriority(registrations[left].Value.Hooks, name)
		rightPriority := hookPriority(registrations[right].Value.Hooks, name)
		if leftPriority == rightPriority {
			return registrations[left].PluginID < registrations[right].PluginID
		}
		return leftPriority < rightPriority
	})
	return registrations
}

// registeredHook 取插件注册信息中指定名称的钩子
func (m *Manager) registeredHook(pluginID, name string) (RegisteredHook, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	registration, ok := m.registrations[pluginID]
	if !ok {
		return RegisteredHook{}, false
	}
	for _, hook := range registration.Hooks {
		if hook.Name == name {
			return hook, true
		}
	}
	return RegisteredHook{}, false
}

// matchingRoute 在所有已加载插件中查找命中该请求的路由
// 多个命中时取优先级最小者，同优先级按插件 ID 排序
func (m *Manager) matchingRoute(method, requestPath string) (registeredRoute, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	routes := make([]registeredRoute, 0)
	for pluginID, registration := range m.registrations {
		if m.modules[pluginID] == nil {
			continue
		}
		for _, route := range registration.Routes {
			if route.Method != method {
				continue
			}
			matched, params := dynamicPathParams(route.Path, requestPath)
			if matched {
				routes = append(routes, registeredRoute{PluginID: pluginID, Route: route, Params: params})
			}
		}
	}
	if len(routes) == 0 {
		return registeredRoute{}, false
	}
	sort.SliceStable(routes, func(left, right int) bool {
		if routes[left].Route.Priority == routes[right].Route.Priority {
			return routes[left].PluginID < routes[right].PluginID
		}
		return routes[left].Route.Priority < routes[right].Route.Priority
	})
	return routes[0], true
}

// matchingMiddleware 返回指定阶段且路径匹配的已注册中间件
// 按优先级升序，同优先级按插件 ID 稳定排序
func (m *Manager) matchingMiddleware(stage, requestPath string) []registeredMiddleware {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]registeredMiddleware, 0)
	for pluginID, registration := range m.registrations {
		if m.modules[pluginID] == nil {
			continue
		}
		for _, middleware := range registration.Middleware {
			if middleware.Stage == stage && middlewareMatches(middleware, requestPath) {
				result = append(result, registeredMiddleware{PluginID: pluginID, Middleware: middleware})
			}
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Middleware.Priority == result[right].Middleware.Priority {
			return result[left].PluginID < result[right].PluginID
		}
		return result[left].Middleware.Priority < result[right].Middleware.Priority
	})
	return result
}

// hookPriority 取指定钩子的优先级，未注册时为 0
func hookPriority(hooks []RegisteredHook, name string) int {
	for _, hook := range hooks {
		if hook.Name == name {
			return hook.Priority
		}
	}
	return 0
}

// hookCallback 取指定钩子的回调名，未声明回调时回落到钩子名
func hookCallback(hooks []RegisteredHook, name string) string {
	for _, hook := range hooks {
		if hook.Name == name {
			if hook.Callback != "" {
				return hook.Callback
			}
			return name
		}
	}
	return name
}

// middlewareMatches 判断中间件是否作用于该请求路径
// PathPrefix 为空表示匹配所有路径
func middlewareMatches(middleware RegisteredMiddleware, requestPath string) bool {
	return middleware.PathPrefix == "" || strings.HasPrefix(requestPath, middleware.PathPrefix)
}

// dynamicPathMatches 判断请求路径是否命中动态路由模式
func dynamicPathMatches(pattern, requestPath string) bool {
	matched, _ := dynamicPathParams(pattern, requestPath)
	return matched
}

// dynamicPathParams 解析动态路由模式并返回路径参数
// 模式以 :name 声明单段参数，仅末尾的 * 匹配剩余全部段
func dynamicPathParams(pattern, requestPath string) (bool, map[string]string) {
	if pattern == requestPath || pattern == "/*" {
		return true, map[string]string{}
	}
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	requestParts := strings.Split(strings.Trim(requestPath, "/"), "/")
	params := make(map[string]string)
	for index, part := range patternParts {
		if part == "*" && index == len(patternParts)-1 {
			return len(requestParts) >= index, params
		}
		if index >= len(requestParts) {
			return false, nil
		}
		if strings.HasPrefix(part, ":") {
			params[strings.TrimPrefix(part, ":")] = requestParts[index]
			continue
		}
		if part != requestParts[index] {
			return false, nil
		}
	}
	return len(patternParts) == len(requestParts), params
}

// PluginAdminPage 是已注册管理页在 Web 控制台菜单中的投影
type PluginAdminPage struct {
	PluginID string
	Slug     string
	Label    string
	Parent   string
	Position int
}

// AdminPages 返回已加载插件注册的管理页投影
// 按 Position 升序，同位置按插件 ID 与 slug 排序
func (m *Manager) AdminPages() []PluginAdminPage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]PluginAdminPage, 0)
	for pluginID, registration := range m.registrations {
		if m.modules[pluginID] == nil {
			continue
		}
		for _, page := range registration.AdminPages {
			result = append(result, PluginAdminPage{
				PluginID: pluginID,
				Slug:     page.Slug,
				Label:    page.Label,
				Parent:   page.Parent,
				Position: page.Position,
			})
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Position == result[right].Position {
			return result[left].PluginID+result[left].Slug < result[right].PluginID+result[right].Slug
		}
		return result[left].Position < result[right].Position
	})
	return result
}
