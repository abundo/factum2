package main

// ---------------------------------------------------------------------------
//
// This program is meant to run on a host with network access to the
// devices - it needs no direct database access, only the Factum API
// (factum.url/factum.token, see internal/factum.NewFactumClient,
// internal/netbox.RemoteClient and internal/device-sync.FetchRemoteConfig).
//
// ---------------------------------------------------------------------------

import (
	"os"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	devicesync "github.com/abundo/factum2/internal/device-sync"
	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
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

	boa.CmdT[Params]{
		Use:   "factum-device-sync",
		Short: "Sync device interfaces/addresses/connections with Netbox",

		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(),
			boa.CmdT[SyncParams]{
				Use:   "sync",
				Short: "Sync devices with Netbox",
				RunFuncE: func(p *SyncParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)

					// nb is write-only now: reads come from factum (already
					// synced from Netbox), see internal/device-sync's package
					// doc comment.
					nb, err := netbox.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					factumClient := factum.NewFactumClient(&p.Config.Factum)
					cfg, err := devicesync.FetchRemoteConfig(&p.Config.Factum)
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
	}.Run()
}
