package main

// ---------------------------------------------------------------------------
//
// This program can be run either on primary or a remote server. "start"
// listens for commands dispatched by the primary over the hub transport
// (internal/worker/hub_agent.go); "run" asks the primary to dispatch a
// command and streams the result back, talking to it over REST
// (factum.url/.token) rather than any direct worker-to-worker connection.
//
// ---------------------------------------------------------------------------

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/worker"
	"github.com/spf13/cobra"
)

// Params embeds cmdbase.ParamsAgent, not cmdbase.Params - factum2-worker
// never reads the db/web/librenms sections of the full util.ConfigRoot
// (internal/worker only ever touches ConfigWorker), so
// there's no reason to force an operator to fill in unrelated required
// fields (web.jwtsecret etc.) just to run this binary.
type Params struct {
	cmdbase.ParamsAgent
}

type RunParams struct {
	Params
	Command string `descr:"Name of the predefined command to run"`
}

func main() {
	cmdbase.SetupCLI()

	boa.CmdT[boa.NoParams]{
		Use:     "factum2-worker",
		Short:   "Run factum background tasks dispatched by the primary over the hub transport",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(),

			boa.CmdT[Params]{
				Use:   "start",
				Short: "Start the worker (commands per worker.commands in configuration) and run until stopped",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)

					ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
					defer stop()

					w := worker.New(&p.Config.Worker)
					return w.Start(ctx)
				},
			},

			boa.CmdT[RunParams]{
				Use:   "run",
				Short: "Ask the primary to dispatch a predefined command to every agent configured for it and stream output back",
				RunFuncE: func(p *RunParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)

					ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
					defer stop()

					exitCode, err := worker.RunRemote(ctx, &p.Config.Factum, p.Command, args, func(msg worker.LogMsg) {
						switch msg.Stream {
						case worker.StreamStdout:
							fmt.Println(msg.Data)
						case worker.StreamStderr:
							fmt.Fprintln(os.Stderr, msg.Data)
						}
					})
					if err != nil {
						return err
					}
					if exitCode != 0 {
						os.Exit(exitCode)
					}
					return nil
				},
			},
		),
	}.Run()
}
