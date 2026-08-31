package main

// ---------------------------------------------------------------------------
//
// This program can be run on the primary server.
// Requires access to the BECS JSON-RPC API and the Netbox API.
//
// ---------------------------------------------------------------------------

import (
	"os"

	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/becs"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"

	"github.com/GiGurra/boa/pkg/boa"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.Params
}

type ParamsGetElement struct {
	Params
	Name string
}

type ParamsSync struct {
	Params
	Name string `required:"false"`
	Job  bool   `descr:"Emit structured job events (JSON lines) on stdout instead of human-readable output" optional:"true"`
}

func main() {
	cmdbase.SetupCLI()

	boa.CmdT[boa.NoParams]{
		Use:     "factum2-becs",
		Short:   "Manage BECS",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfig(),
			boa.CmdT[ParamsGetElement]{
				Use:   "get-element",
				Short: "Get one element from BECS",
				RunFuncE: func(p *ParamsGetElement, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					db, err := util.ConnectDatabase(&p.Config.DB)
					if err != nil {
						return err
					}
					settings, err := util.GetOrCreateSettings(db)
					if err != nil {
						return err
					}
					client, err := becs.NewClientFromSettings(settings)
					if err != nil {
						return err
					}
					data, err := client.GetElement(p.Name, int(settings.BecsEapiOID))
					if err != nil {
						return err
					}
					if settings.DefaultDomain != "" {
						data.ShortName = util.ShortName(data.ShortName, settings.DefaultDomain)
						data.Name = util.FormatName(settings.DefaultDomain, data.ShortName)
					}
					util.Pprint(data)
					return nil
				},
			},
			boa.CmdT[ParamsSync]{
				Use:   "sync",
				Short: "Sync BECS elements into Netbox, then into factum",
				RunFuncE: func(p *ParamsSync, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					return becs.Sync(&p.Config, p.Name, reporter)
				},
			},
		),
	}.Run()
}
