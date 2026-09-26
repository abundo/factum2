package certs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
)

type Client struct {
	Config *util.ConfigAgentRoot
	Certs  *Config
	run    func(bin string, dir string) error
}

func NewClient(config *util.ConfigAgentRoot) (*Client, error) {
	c := &Client{Config: config}
	var err error
	c.Certs, err = FetchRemoteConfig(&config.Factum)
	if err != nil {
		return nil, err
	}
	if err := applyWorkerPaths(c.Certs, config.Worker.Paths); err != nil {
		return nil, err
	}
	return c, nil
}

func applyWorkerPaths(cfg *Config, paths util.ConfigWorkerPaths) error {
	bin, err := util.PinnedExecutable(paths.LegoBin, cfg.LegoBin, "worker.paths.lego_bin")
	if err != nil {
		return err
	}
	cfg.LegoBin = bin
	yamlPath, err := util.PinnedPath(paths.LegoYaml, cfg.LegoYaml, "worker.paths.lego_yaml")
	if err != nil {
		return err
	}
	cfg.LegoYaml = yamlPath
	envPath, err := util.PinnedPath(paths.LegoEnv, cfg.EnvFile, "worker.paths.lego_env")
	if err != nil {
		return err
	}
	cfg.EnvFile = envPath
	storage, err := util.PinnedPath(paths.LegoStorage, cfg.LegoStorage, "worker.paths.lego_storage")
	if err != nil {
		return err
	}
	cfg.LegoStorage = storage
	return nil
}

func (c *Client) Sync(reporter jobevent.Reporter) error {
	reporter.Emit(jobevent.Info, "Certificate sync started")
	if c.Certs == nil {
		err := fmt.Errorf("certs config is not loaded")
		reporter.EmitErr(err)
		return err
	}
	if len(c.Certs.Certificates) == 0 {
		reporter.Emit(jobevent.Warning, "No certificates defined")
	}
	ApplyConfigDefaults(c.Certs)
	yamlPath, envPath, err := WriteFiles(c.Certs)
	if err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Wrote %s and %s", yamlPath, envPath)

	bin := strings.TrimSpace(c.Certs.LegoBin)
	dir := filepath.Dir(yamlPath)
	run := c.run
	if run == nil {
		run = func(bin, dir string) error {
			cmd := exec.Command(bin)
			cmd.Dir = dir
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
	}
	reporter.Emit(jobevent.Info, "Running %s in %s", bin, dir)
	if err := run(bin, dir); err != nil {
		reporter.EmitErr(err)
		return err
	}
	reporter.Emit(jobevent.Info, "Certificate sync finished")
	return nil
}
