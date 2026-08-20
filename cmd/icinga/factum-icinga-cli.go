package main

// ---------------------------------------------------------------------------
//
// This program can be run on icinga server
// Requires access to Factum API, Icinga API and Icinga configuration files
//
// ---------------------------------------------------------------------------

import (
	"os"

	cmdbase "github.com/abundo/factum2/cmd"

	"github.com/GiGurra/boa/pkg/boa"
	"github.com/abundo/factum2/internal/icinga"
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

	boa.CmdT[boa.NoParams]{
		Use:   "factum-icinga",
		Short: "Manage Icinga",
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(func(fc *util.ConfigFactum) (any, error) { return icinga.FetchRemoteConfig(fc) }),

			boa.CmdT[Params]{
				Use:   "get-hosts-down",
				Short: "Get all hosts in state down and not acknowledged",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					icinga, err := icinga.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					res, err := icinga.GetHostsDown()
					if err != nil {
						return err
					}
					util.Pprint(res)
					return nil
				},
			},
			boa.CmdT[Params]{
				Use:   "get-services-down",
				Short: "Get all services in state down and not acknowledged",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					icinga, err := icinga.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					res, err := icinga.GetServicesDown()
					if err != nil {
						return err
					}
					util.Pprint(res)
					return nil
				},
			},
			boa.CmdT[Params]{
				Use:   "show-events",
				Short: "Show events until CTRL-C pressed",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					icinga, err := icinga.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					// def signal_handler(sig, frame):
					//     """Trap ctrl-c, do to proper termination"""
					//     global running
					//     running = False

					// signal.signal(signal.SIGINT, signal_handler)
					// print("Streaming events")
					// r = icinga.get_events()
					// for line in r.iter_lines(decode_unicode=True):
					//     if running:
					//         if line:
					//             data = json.loads(line)
					//             abutils.pprint(data)
					//     else:
					//         break
					util.Pprint(icinga)

					return nil
				},
			},
			boa.CmdT[SyncParams]{
				Use:   "sync",
				Short: "Sync Icinga with factum",
				RunFuncE: func(p *SyncParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					factumIcingaClient, err := icinga.NewFactumIcingaClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					err = factumIcingaClient.Sync(reporter)
					if err != nil {
						return err
					}
					return nil
				},
			},
		),
	}.Run()
}
