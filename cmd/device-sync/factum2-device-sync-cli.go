package main

// ---------------------------------------------------------------------------
//
// This program runs on the primary (same host as factum2-web). It needs no
// direct database access, only the Factum REST API (factum.url / factum.token).
// It does not use the worker hub unix socket — even when factum2-worker is
// co-located — see util.WithoutHubSocket.
//
// ---------------------------------------------------------------------------

import (
	"os"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	devicesync "github.com/abundo/factum2/internal/device-sync"
	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.ParamsAgent
}

type SyncParams struct {
	Params
	Job        bool   `descr:"Emit structured job events (JSON lines) on stdout instead of human-readable output" optional:"true"`
	Name       string `descr:"Sync only the device with this name" optional:"true"`
	Platform   string `descr:"Sync only devices with this Netbox platform" optional:"true"`
	Unattended bool   `descr:"Don't prompt before deleting an interface or address" optional:"true"`
}

func main() {
	cmdbase.SetupCLI()

	cmdbase.Run(boa.CmdT[Params]{
		Use:     "factum2-device-sync",
		Version: buildinfo.Version,
		Short:   "Sync device interfaces/addresses/connections with Netbox",

		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(),
			boa.CmdT[SyncParams]{
				Use:   "sync",
				Short: "Sync devices with Netbox",
				RunFuncE: func(p *SyncParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)

					// REST to factum2-web, not the co-located worker socket.
					factumCfg := util.WithoutHubSocket(p.Config.Factum)

					// nb is write-only now: reads come from factum (already
					// synced from Netbox), see internal/device-sync's package
					// doc comment.
					nb, err := netbox.RemoteClient(&factumCfg)
					if err != nil {
						return err
					}
					factumClient := factum.NewFactumClient(&factumCfg)
					cfg, err := devicesync.FetchRemoteConfig(&factumCfg)
					if err != nil {
						return err
					}

					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}

					return devicesync.Sync(nb, factumClient, cfg, reporter, devicesync.SyncOptions{
						Name:     p.Name,
						Platform: p.Platform,
						// A --job run's stdout is JSON lines, not a place
						// to put an interactive y/n prompt.
						Unattended: p.Unattended || p.Job,
					})
				},
			}),
	})
}
