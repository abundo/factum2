package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	devicesync "github.com/abundo/factum2/internal/device-sync"
	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/storage"
	"github.com/abundo/factum2/internal/util"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.ParamsAgent
}

type pathParams struct {
	Params
	Path string `short:"p" descr:"Repository path" optional:"false"`
}

type moveParams struct {
	Params
	From string `descr:"Source path" optional:"false"`
	To   string `descr:"Destination path" optional:"false"`
}

type copyParams struct {
	Params
	Name        string `short:"n" descr:"Device name" optional:"false"`
	Path        string `descr:"Repository file path" optional:"false"`
	Protocol    string `alts:"http,tftp,scp,sftp" optional:"false"`
	Destination string `descr:"On-device destination path (default: basename)" optional:"true"`
	Job         bool   `descr:"Emit structured job events on stdout" optional:"true"`
}

func main() {
	cmdbase.SetupCLI()

	cmdbase.Run(boa.CmdT[boa.NoParams]{
		Use:     "factum2-storage",
		Short:   "Software image repository and copy-to-device",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(func(fc *util.ConfigFactum) (any, error) { return storage.FetchRemoteConfig(fc) }),
			boa.CmdT[Params]{
				Use:   "start",
				Short: "Serve the repository (unix API plus device HTTP/TFTP/SFTP)",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					cfg, err := storage.FetchRemoteConfig(&p.Config.Factum)
					if err != nil {
						return err
					}
					repo, err := storage.NewRepo(cfg.Root)
					if err != nil {
						return err
					}
					ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
					defer stop()
					return (&storage.Server{
						Repo:   repo,
						Config: *cfg,
						Socket: storage.StorageSocketPath(p.Config.Storage.Socket),
					}).Run(ctx)
				},
			},
			boa.CmdT[pathParams]{
				Use:   "ls",
				Short: "List a repository directory",
				RunFuncE: func(p *pathParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					repo, err := openRepo(&p.Params)
					if err != nil {
						return err
					}
					ents, err := repo.List(p.Path)
					if err != nil {
						return err
					}
					util.Pprint(ents)
					return nil
				},
			},
			boa.CmdT[pathParams]{
				Use:   "mkdir",
				Short: "Create a repository directory",
				RunFuncE: func(p *pathParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					repo, err := openRepo(&p.Params)
					if err != nil {
						return err
					}
					return repo.Mkdir(p.Path)
				},
			},
			boa.CmdT[pathParams]{
				Use:   "rm",
				Short: "Delete a repository file or empty directory",
				RunFuncE: func(p *pathParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					repo, err := openRepo(&p.Params)
					if err != nil {
						return err
					}
					return repo.Remove(p.Path)
				},
			},
			boa.CmdT[moveParams]{
				Use:   "mv",
				Short: "Move or rename a repository path",
				RunFuncE: func(p *moveParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					repo, err := openRepo(&p.Params)
					if err != nil {
						return err
					}
					return repo.Move(p.From, p.To)
				},
			},
			boa.CmdT[copyParams]{
				Use:   "copy",
				Short: "Copy a repository file to a device (http/tftp pull, scp/sftp push)",
				RunFuncE: func(p *copyParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					if err := drivers.InitSSHPoolFromDriver(p.Config.Driver); err != nil {
						return err
					}
					defer drivers.CloseSSHPool()
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					return runCopy(&p.Params, reporter, p.Name, p.Path, p.Protocol, p.Destination)
				},
			},
		),
	})
}

func openRepo(p *Params) (*storage.Repo, error) {
	cfg, err := storage.FetchRemoteConfig(&p.Config.Factum)
	if err != nil {
		return nil, err
	}
	return storage.NewRepo(cfg.Root)
}

func runCopy(p *Params, reporter jobevent.Reporter, name, rel, protocol, dest string) error {
	protocol, err := storage.NormalizeProtocol(protocol)
	if err != nil {
		return err
	}
	cfg, err := storage.FetchRemoteConfig(&p.Config.Factum)
	if err != nil {
		return err
	}
	repo, err := storage.NewRepo(cfg.Root)
	if err != nil {
		return err
	}
	if _, err := repo.Stat(rel); err != nil {
		return err
	}
	ds, err := devicesync.FetchRemoteConfig(&p.Config.Factum)
	if err != nil {
		return err
	}
	user, pass, err := lookupAuth(ds.Auth, name)
	if err != nil {
		return err
	}
	device, err := factum.NewFactumClient(&p.Config.Factum).GetDeviceByName(name)
	if err != nil {
		return err
	}
	host := drivers.DeviceFQDN(device.Name, cfg.DefaultDomain)
	req := storage.CopyRequest{
		Path:        rel,
		Device:      name,
		Protocol:    protocol,
		Destination: dest,
		Username:    user,
		Password:    pass,
		Platform:    device.Platform,
		Host:        host,
	}
	reporter.Emit(jobevent.Info, "copy %s → %s (%s)", rel, name, protocol)
	switch protocol {
	case "http", "tftp":
		src, err := storage.SourceURL(cfg, protocol, rel)
		if err != nil {
			return err
		}
		reporter.Emit(jobevent.Info, "device pull %s", src)
		out, err := storage.PullToDevice(req, src)
		if strings.TrimSpace(out) != "" {
			reporter.Emit(jobevent.Info, "%s", out)
		}
		if err != nil {
			reporter.EmitErr(err)
			return err
		}
	case "scp", "sftp":
		reporter.Emit(jobevent.Info, "push from storage host")
		if err := storage.PushToDevice(repo, req); err != nil {
			reporter.EmitErr(err)
			return err
		}
	}
	reporter.Emit(jobevent.Info, "copy finished")
	return nil
}

func lookupAuth(auth map[string]util.ConfigDeviceSyncAuth, name string) (string, string, error) {
	if a, ok := auth[name]; ok && a.Username != "" && a.Password != "" {
		return a.Username, a.Password, nil
	}
	if a, ok := auth["default"]; ok && a.Username != "" && a.Password != "" {
		return a.Username, a.Password, nil
	}
	return "", "", fmt.Errorf("no device-sync credentials for %q (and no default)", name)
}
