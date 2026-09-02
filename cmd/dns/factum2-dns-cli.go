package main

// ---------------------------------------------------------------------------
//
// This program can be run either on primary or a remote server
// Requires access to Factum API
//
// ---------------------------------------------------------------------------

import (
	"os"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/dns"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/util"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.ParamsAgent
}

type SyncParams struct {
	Params
	Job bool `descr:"Emit structured job events (JSON lines) on stdout instead of human-readable output" optional:"true"`
}

func main() {
	cmdbase.SetupCLI()

	cmdbase.Run(boa.CmdT[Params]{
		Use:     "factum2-dns",
		Short:   "Manage DNS",
		Version: buildinfo.Version,

		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(func(fc *util.ConfigFactum) (any, error) { return dns.FetchRemoteConfig(fc) }),
			boa.CmdT[SyncParams]{
				Use:   "sync",
				Short: "Sync DNS with factum",
				RunFuncE: func(p *SyncParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					d, err := dns.NewDNSClient(&p.Config)
					if err != nil {
						return err
					}
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					err = d.Sync(reporter)
					return err
				},
			}),
	})
}
