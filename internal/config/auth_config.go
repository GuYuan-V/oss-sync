package config

import "fmt"

// defaultWebSessionTTLHours 是 Web 会话有效期未配置时的默认小时数
const defaultWebSessionTTLHours = 24

// defaultDeviceJWTTTLHours 是设备 JWT 有效期未配置时的默认小时数
const defaultDeviceJWTTTLHours = 30 * 24

// EffectiveWebSessionTTLHours 返回 Web 会话有效期，未配置或为 0 时取默认值
func (c AuthConfig) EffectiveWebSessionTTLHours() int {
	if c.WebSessionTTLHours == 0 {
		return defaultWebSessionTTLHours
	}
	return c.WebSessionTTLHours
}

// EffectiveDeviceJWTTTLHours 返回设备 JWT 有效期，未配置或为 0 时取默认值
func (c AuthConfig) EffectiveDeviceJWTTTLHours() int {
	if c.DeviceJWTTTLHours == 0 {
		return defaultDeviceJWTTTLHours
	}
	return c.DeviceJWTTTLHours
}

// validate 校验认证配置，两个有效期均不允许为负数
func (c AuthConfig) validate() error {
	if c.WebSessionTTLHours < 0 {
		return fmt.Errorf("auth.web_session_ttl_hours 不能为负数，收到 %d", c.WebSessionTTLHours)
	}
	if c.DeviceJWTTTLHours < 0 {
		return fmt.Errorf("auth.device_jwt_ttl_hours 不能为负数，收到 %d", c.DeviceJWTTTLHours)
	}
	return nil
}
