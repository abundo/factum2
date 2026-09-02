package main

// ---------------------------------------------------------------------------
//
// This program can be run on the prometheus / snmp_exporter server
// Requires access to Factum API; writes Prometheus file_sd for snmp_exporter
//
// ---------------------------------------------------------------------------

import (
	"os"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/prometheus"
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

	cmdbase.Run(boa.CmdT[boa.NoParams]{
		Use:     "factum2-prometheus",
		Short:   "Manage Prometheus snmp_exporter targets",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(func(fc *util.ConfigFactum) (any, error) { return prometheus.FetchRemoteConfig(fc) }),

			boa.CmdT[SyncParams]{
				Use:   "sync",
				Short: "Sync Prometheus snmp_exporter targets with factum",
				RunFuncE: func(p *SyncParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					client, err := prometheus.NewFactumPrometheusClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					return client.Sync(reporter)
				},
			},
		),
	})
}
