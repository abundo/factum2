package main

// ---------------------------------------------------------------------------
//
// This program can be run on oxidized server
// Requires access to Factum API, oxizided API, local oxidized config files
//
// ---------------------------------------------------------------------------

import (
	"os"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/oxidized"
	"github.com/abundo/factum2/internal/util"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.ParamsAgent
}

type NameParams struct {
	Params
	Name string `short:"n" descr:"Device name" optional:"false"`
}

type SyncParams struct {
	Params
	Job bool `descr:"Emit structured job events (JSON lines) on stdout instead of human-readable output" optional:"true"`
}

func main() {
	cmdbase.SetupCLI()

	boa.CmdT[boa.NoParams]{
		Use:     "factum2-oxidized",
		Short:   "Manage oxidized",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(func(fc *util.ConfigFactum) (any, error) { return oxidized.FetchRemoteConfig(fc) }),

			boa.CmdT[NameParams]{
				Use:   "get-device",
				Short: "Get a single device from oxidized's router.db",
				RunFuncE: func(p *NameParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					ox, err := oxidized.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					device, err := ox.GetDevice(p.Name)
					if err != nil {
						return err
					}
					util.Pprint(device)
					return nil
				},
			},
			boa.CmdT[Params]{
				Use:   "get-devices",
				Short: "Get all devices from oxidized's router.db",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					ox, err := oxidized.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					devices, err := ox.GetDevices()
					if err != nil {
						return err
					}
					util.Pprint(devices)
					return nil
				},
			},
			boa.CmdT[NameParams]{
				Use:   "get-device-config",
				Short: "Fetch a device's last stored configuration from oxidized",
				RunFuncE: func(p *NameParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					ox, err := oxidized.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					conf, err := ox.GetDeviceConfig(p.Name)
					if err != nil {
						return err
					}
					util.Pprint(conf)
					return nil
				},
			},
			boa.CmdT[Params]{
				Use:   "reload",
				Short: "Ask oxidized to reload its router.db",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					ox, err := oxidized.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					_, err = ox.Reload()
					return err
				},
			},
			boa.CmdT[SyncParams]{
				Use:   "sync",
				Short: "Sync Oxidized with factum",
				RunFuncE: func(p *SyncParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					factumOxidizedClient, err := oxidized.NewFactumOxidizedClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					return factumOxidizedClient.Sync(reporter)
				},
			},
		),
	}.Run()
}
