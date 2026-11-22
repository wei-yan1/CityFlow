package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeromicro/go-zero/core/conf"
)

func TestXXLJobSettingsLoadAsBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.yaml")
	body := []byte(`Name: test.rpc
ListenOn: 127.0.0.1:0
Mode: dev
Timeout: 10000
XXLJob:
  Enabled: true
  ServerAddr: "http://127.0.0.1:8080/xxl-job-admin"
  RegistryKey: "cityflow-task"
  ExecutorPort: "9999"
  TimeoutSeconds: 7
`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var c Config
	conf.MustLoad(path, &c)
	if !c.XXLJob.Enabled || c.XXLJob.RegistryKey != "cityflow-task" || c.XXLJob.TimeoutSeconds != 7 {
		t.Fatalf("nested XXLJob settings were not loaded: %+v", c.XXLJob)
	}
	if c.XXLJob.ExecutorPort != "9999" {
		t.Fatalf("unexpected executor port: %q", c.XXLJob.ExecutorPort)
	}
}
