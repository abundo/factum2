package librenms

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDbConfigFromLibrenmsEnvDockerStyle(t *testing.T) {
	cfg := dbConfigFromLibrenmsEnv(map[string]string{
		"MYSQL_DATABASE": "librenms",
		"MYSQL_USER":     "librenms",
		"MYSQL_PASSWORD": "secret",
		"MYSQL_PORT":     "13306",
		"MYSQL_HOST":     "127.0.0.1",
	})
	if cfg.Host != "127.0.0.1" || cfg.Port != "13306" || cfg.Database != "librenms" || cfg.User != "librenms" || cfg.Pass != "secret" {
		t.Fatalf("docker-style env: %+v", cfg)
	}
}

func TestDbConfigFromLibrenmsEnvDockerStyleDefaultHost(t *testing.T) {
	cfg := dbConfigFromLibrenmsEnv(map[string]string{
		"MYSQL_DATABASE": "librenms",
		"MYSQL_USER":     "librenms",
		"MYSQL_PASSWORD": "secret",
	})
	if cfg.Host != "127.0.0.1" || cfg.Port != "3306" {
		t.Fatalf("defaults: %+v", cfg)
	}
}

func TestDbConfigFromLibrenmsEnvNativeStyle(t *testing.T) {
	cfg := dbConfigFromLibrenmsEnv(map[string]string{
		"DB_HOST":     "mysql",
		"DB_DATABASE": "librenms",
		"DB_USERNAME": "lnms",
		"DB_PASSWORD": "pw",
		"DB_PORT":     "3307",
	})
	if cfg.Host != "mysql" || cfg.Port != "3307" || cfg.User != "lnms" {
		t.Fatalf("native-style env: %+v", cfg)
	}
}

func TestReadLibrenmsDBConfigEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("MYSQL_DATABASE=librenms\nMYSQL_USER=u\nMYSQL_PASSWORD=p\nMYSQL_PORT=13306\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIBRENMS_ENV_FILE", path)
	cfg, err := readLibrenmsDBConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database != "librenms" || cfg.Port != "13306" || cfg.Host != "127.0.0.1" {
		t.Fatalf("got %+v", cfg)
	}
}
