package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedUpdateCapability(t *testing.T) {
	withVersion(t, "1.2.3")
	t.Setenv("OSS_UPDATE_MANAGER", "systemd")
	t.Setenv("OSS_UPDATE_VIA_PATH_UNIT", "1")
	if !ManagedUpdateEnabled() {
		t.Fatal("ManagedUpdateEnabled 在 systemd + path unit 开关下应为 true")
	}
	if err := CheckCurrentCapability(regularFile(t)); !IsManagedUpdateError(err) {
		t.Fatalf("期望托管更新，得到 %v", err)
	}
	// 未设开关时仍要求外部更新，保证旧部署不受影响
	t.Setenv("OSS_UPDATE_VIA_PATH_UNIT", "")
	if err := CheckCurrentCapability(regularFile(t)); !IsExternalUpdateError(err) {
		t.Fatalf("无开关时期望外部更新，得到 %v", err)
	}
}

func TestManagedRequestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if ManagedRequestPending(dir) {
		t.Fatal("初始不应有待处理请求")
	}
	if err := WriteManagedRequest(dir, "op-1", "1.2.3"); err != nil {
		t.Fatalf("WriteManagedRequest: %v", err)
	}
	if !ManagedRequestPending(dir) {
		t.Fatal("写入后应有待处理请求")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".update", "request"))
	if err != nil {
		t.Fatalf("读取请求: %v", err)
	}
	for _, want := range []string{`"op_id":"op-1"`, `"target_version":"1.2.3"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("请求缺少 %s: %s", want, data)
		}
	}
	if _, ok := ReadManagedResult(dir); ok {
		t.Fatal("尚不应存在结果")
	}
	resPath := filepath.Join(dir, ".update", "result.json")
	if err := os.WriteFile(resPath, []byte(`{"op_id":"op-1","version":"1.2.3","status":"success"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	res, ok := ReadManagedResult(dir)
	if !ok || res.Status != "success" || res.Version != "1.2.3" || res.OpID != "op-1" {
		t.Fatalf("结果不符: %+v ok=%v", res, ok)
	}
}
