package main

// ---------------------------------------------------------------------------
//
// This program can be run on primary server
// Requires access to Lime API
//
// ---------------------------------------------------------------------------

import (
	"os"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/lime"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.Params
}

type ParamsRefresh struct {
	Params
	Company []string `descr:"Company name to fetch (repeatable). If not specified, all companies are fetched" short:"c" optional:"true"`
	Refresh bool     `descr:"Refresh data in cache from Lime" short:"r" optional:"true"`
	Job     bool     `descr:"Emit structured job events (JSON lines) on stdout instead of human-readable output" optional:"true"`
}

func main() {
	cmdbase.SetupCLI()

	cmdbase.Run(boa.CmdT[ParamsRefresh]{
		Use:     "factum2-lime",
		Short:   "lime",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfig(),

			boa.CmdT[ParamsRefresh]{
				Use:   "sync",
				Short: "Sync lime with factum",
				RunFuncE: func(p *ParamsRefresh, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					companies := p.Company
					if len(companies) == 0 {
						companies = []string{""}
					}
					l := lime.NewLime(&p.Config.DB)
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					err := l.SyncCustomers(companies, p.Refresh, reporter)
					return err
				},
			},
		),
	})
}
