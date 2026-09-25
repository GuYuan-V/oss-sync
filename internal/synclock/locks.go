// Package synclock 提供进程内 Vault 锁与路径锁
package synclock

import "sync"

var (
	vaultLocks sync.Map
	pathLocks  sync.Map
)

// Vault 返回进程内共享的 Vault 锁
func Vault(vaultID string) *sync.Mutex {
	value, _ := vaultLocks.LoadOrStore(vaultID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// Path 返回进程内共享的文件路径锁
func Path(key string) *sync.Mutex {
	value, _ := pathLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}
