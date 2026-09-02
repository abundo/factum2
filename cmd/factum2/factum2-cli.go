package main

// ---------------------------------------------------------------------------
//
// This program can be run either on primary or a remote server
// Requires access to Factum API
//
// ---------------------------------------------------------------------------

import (
	"fmt"
	"strings"
	"time"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/util"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.ParamsAgent
}
type GetDeviceParams struct {
	Params
	Name string
}
type SyncTriggerParams struct {
	Params
	Target string
}

// syncJobTimeout/syncPollInterval bound how long we wait for a job's tasks
// to all report finished - matches the frontend's 3s job-list poll
// (SyncOverviewPage.vue), just faster, since this is a blocking CLI call
// rather than a background UI refresh. syncJobTimeout covers both a
// single-target trigger and "sync all": since worker.RemoteManager now
// dispatches a batch's targets one at a time instead of all at once (see
// StartJob), "sync all" waits for roughly sum(subjob durations) rather
// than max(subjob durations), so this needs to be generous enough to
// cover every enabled target's sync back-to-back, not just the slowest
// one.
const (
	syncJobTimeout   = 30 * time.Minute
	syncPollInterval = 1500 * time.Millisecond
)

// waitForJob polls job history until jobID's row has FinishedAt set - a
// Job's FinishedAt is only stamped once every one of its JobTasks reports
// completion (worker.RemoteManager.resolveTask), so this covers both a
// single-target trigger (one task) and "sync all" (one task per target)
// with the same wait.
func waitForJob(client *factum.FactumClient, jobID uint) (*factum.Job, error) {
	deadline := time.Now().Add(syncJobTimeout)
	for time.Now().Before(deadline) {
		jobs, err := client.GetJobs()
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if job.ID == jobID {
				if job.FinishedAt != nil {
					return &job, nil
				}
				break
			}
		}
		time.Sleep(syncPollInterval)
	}
	return nil, fmt.Errorf("timed out waiting for job %d to finish", jobID)
}

func main() {
	cmdbase.SetupCLI()

	cmdbase.Run(boa.CmdT[boa.NoParams]{
		Use:     "factum2",
		Short:   "Manage Factum",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(),
			cmdbase.Migrate(),

			boa.CmdT[GetDeviceParams]{
				Use: "get-device",
				RunFuncE: func(p *GetDeviceParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					var err error
					factum := factum.NewFactumClient(&p.Config.Factum)
					data, err := factum.GetDevice(p.Name)
					if err != nil {
						return err
					}
					util.Pprint(data)
					return err

				},
			},

			boa.CmdT[Params]{
				Use: "get-devices",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					var err error
					factum := factum.NewFactumClient(&p.Config.Factum)
					data, err := factum.GetDevices()
					if err != nil {
						return err
					}
					util.Pprint(data)
					return err
				},
			},

			boa.CmdT[boa.NoParams]{
				Use:   "sync",
				Short: "Trigger sync jobs",
				SubCmds: boa.SubCmds(
					boa.CmdT[Params]{
						Use:   "targets",
						Short: "List the sync targets currently enabled in Settings",
						RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
							cmdbase.SetupLog(p.CommonParams)
							client := factum.NewFactumClient(&p.Config.Factum)
							targets, err := client.GetSyncTargets()
							if err != nil {
								return err
							}
							util.Pprint(targets)
							return nil
						},
					},

					boa.CmdT[SyncTriggerParams]{
						Use:   "trigger",
						Short: "Trigger a single sync target and wait for it to finish",
						RunFuncE: func(p *SyncTriggerParams, cmd *cobra.Command, args []string) error {
							cmdbase.SetupLog(p.CommonParams)
							client := factum.NewFactumClient(&p.Config.Factum)
							jobID, err := client.TriggerSync(p.Target)
							if err != nil {
								return err
							}
							fmt.Printf("%s: queued (job %d)\n", p.Target, jobID)
							job, err := waitForJob(client, jobID)
							if err != nil {
								return err
							}
							for _, task := range job.Tasks {
								if task.Target == p.Target && task.ExitCode != 0 {
									return fmt.Errorf("%s: sync failed (exit code %d)", p.Target, task.ExitCode)
								}
							}
							fmt.Printf("%s: done\n", p.Target)
							return nil
						},
					},

					// all mirrors SyncOverviewPage.vue's "Sync all" button: one job
					// dispatching every enabled target in turn, sources before
					// destinations (server-side, worker.RemoteManager.StartJob /
					// worker.SequencedSyncAllTargets), waiting for each to finish
					// before starting the next.
					boa.CmdT[Params]{
						Use:   "all",
						Short: `Trigger every enabled sync target as one job - same as the web UI's "Sync all" button`,
						RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
							cmdbase.SetupLog(p.CommonParams)
							client := factum.NewFactumClient(&p.Config.Factum)

							jobID, err := client.TriggerSyncAll()
							if err != nil {
								return err
							}
							fmt.Printf("sync all: queued (job %d)\n", jobID)

							job, err := waitForJob(client, jobID)
							if err != nil {
								return err
							}

							var failed []string
							for _, task := range job.Tasks {
								if task.ExitCode != 0 {
									fmt.Printf("%s: failed (exit code %d)\n", task.Target, task.ExitCode)
									failed = append(failed, task.Target)
									continue
								}
								fmt.Printf("%s: done\n", task.Target)
							}

							if len(failed) > 0 {
								return fmt.Errorf("sync all finished with errors: %s", strings.Join(failed, ", "))
							}
							fmt.Printf("sync all: %d target(s) synced successfully\n", len(job.Tasks))
							return nil
						},
					},
				),
			},
		),
	})

}
