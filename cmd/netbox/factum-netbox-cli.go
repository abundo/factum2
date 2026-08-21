package main

// ---------------------------------------------------------------------------
//
// This program can be run on primary server
// Requires access to Netbox API
//
// ---------------------------------------------------------------------------

import (
	"fmt"
	"os"

	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/netboxtool"

	"github.com/GiGurra/boa/pkg/boa"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/netbox"
	"github.com/abundo/factum2/internal/util"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.Params
}

type GetDeviceParams struct {
	Params
	Name string `descr:"Name" short:"n" optional:"true"`
	ID   int    `descr:"Device id" short:"i" optional:"true"`
}

type DeviceTypeParams struct {
	Params
	Manufacturer string ``
	Model        string ``
}

type DeviceInterfaceParams struct {
	Params
	Name   string
	IFName string
}

type ParamsSync struct {
	Params
	Name string `required:"false"`
	Job  bool   `descr:"Emit structured job events (JSON lines) on stdout instead of human-readable output" optional:"true"`
}

type ParamsCheck struct {
	Params
	Update bool `descr:"Create or update custom fields that are missing or drifted" optional:"true"`
}

// newNetbox connects to the database to read the Netbox API URL/token from
// the Settings table (admin-editable in the web UI) instead of the YAML
// config file, which no longer has a netbox: section.
func newNetbox(c *util.ConfigDB) (*netboxtool.NetboxClient, error) {
	db, err := util.ConnectMigrate(c)
	if err != nil {
		return nil, err
	}
	settings, err := util.GetOrCreateSettings(db)
	if err != nil {
		return nil, err
	}
	return netboxtool.NewNetboxClient(netboxtool.ConfigNetbox{
		URL:   settings.NetboxApiURL,
		Token: settings.NetboxApiToken,
	})
}

func main() {
	cmdbase.SetupCLI()

	boa.CmdT[struct{}]{
		Use:     "factum-netbox",
		Short:   "Manage Netbox",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfig(),

			boa.CmdT[GetDeviceParams]{
				Use: "get-device",
				RunFuncE: func(p *GetDeviceParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					if p.Name == "" && p.ID == 0 {
						fmt.Println("Error:Specify name or id")
						return nil
					}
					nb, err := newNetbox(&p.Config.DB)
					if err != nil {
						return err
					}
					device, err := nb.GetDevice(p.Name, p.ID)
					if err != nil {
						return err
					}
					util.Pprint(device)
					return nil
				},
			},

			boa.CmdT[cmdbase.Params]{
				Use: "get-devices",
				RunFuncE: func(p *cmdbase.Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					nb, err := newNetbox(&p.Config.DB)
					if err != nil {
						return err
					}
					device, err := nb.GetDevices()
					if err != nil {
						return err
					}
					util.Pprint(device)
					return nil
				},
			},

			boa.CmdT[DeviceTypeParams]{
				Use: "get-device-type",
				RunFuncE: func(p *DeviceTypeParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					nb, err := newNetbox(&p.Config.DB)
					if err != nil {
						return err
					}
					deviceType, err := nb.GetDeviceType(p.Manufacturer, p.Model)
					if err != nil {
						return err
					}
					util.Pprint(deviceType)
					return nil
				},
			},
			boa.CmdT[ParamsSync]{
				Use:   "sync",
				Short: "Sync Netbox with factum",
				RunFuncE: func(p *ParamsSync, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					return netbox.Sync(&p.Config, p.Name, reporter)
				},
			},

			boa.CmdT[ParamsCheck]{
				Use:   "check",
				Short: "Verify Netbox webhooks, event rules and custom fields; --update creates/updates fields",
				RunFuncE: func(p *ParamsCheck, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					opts := netbox.CheckOptions{Update: p.Update}
					if err := netbox.Check(&p.Config, opts, jobevent.NewConsoleReporter(os.Stdout)); err != nil {
						// boa.Run treats a plain RunFuncE error as a
						// programming bug and panics. A failed check is
						// expected operational output.
						return boa.NewUserInputError(err)
					}
					return nil
				},
			},
		),
	}.Run()

}
