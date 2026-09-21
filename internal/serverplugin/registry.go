package serverplugin

import (
	"sort"
	"strings"
)

type registeredRoute struct {
	PluginID string
	Route    RegisteredRoute
	Params   map[string]string
}

type registeredMiddleware struct {
	PluginID   string
	Middleware RegisteredMiddleware
}

func (m *Manager) setRegistration(pluginID string, registration ExtensionRegistration) {
	m.registrations[pluginID] = registration
}

func (m *Manager) removeRegistration(pluginID string) {
	delete(m.registrations, pluginID)
}

func (m *Manager) Registrations() map[string]ExtensionRegistration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]ExtensionRegistration, len(m.registrations))
	for pluginID, registration := range m.registrations {
		result[pluginID] = registration
	}
	return result
}

func (m *Manager) RegistrationFor(pluginID string) (ExtensionRegistration, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	registration, ok := m.registrations[pluginID]
	return registration, ok
}

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

func hookPriority(hooks []RegisteredHook, name string) int {
	for _, hook := range hooks {
		if hook.Name == name {
			return hook.Priority
		}
	}
	return 0
}

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

func middlewareMatches(middleware RegisteredMiddleware, requestPath string) bool {
	return middleware.PathPrefix == "" || strings.HasPrefix(requestPath, middleware.PathPrefix)
}

func dynamicPathMatches(pattern, requestPath string) bool {
	matched, _ := dynamicPathParams(pattern, requestPath)
	return matched
}

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
