package main

// ---------------------------------------------------------------------------
//
// CLI for drivers
//
// This program can be run either on primary or a remote server, e.g. one
// with network access to the devices - it needs no database access, only
// the Factum API (factum.url/factum.token), see drivers.NewDriverName
//
// ---------------------------------------------------------------------------

import (
	"errors"

	"github.com/GiGurra/boa/pkg/boa"
	"github.com/spf13/cobra"

	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/drivers"
	"github.com/abundo/factum2/internal/factum"
	"github.com/abundo/factum2/internal/util"
	"github.com/abundo/netboxtool"
)

// Params embeds cmdbase.ParamsAgent, not cmdbase.Params - this binary only
// needs the factum section of the config (it reaches the device inventory
// over REST), so requiring a full ConfigRoot would force db/web/librenms
// values into the config file of every host running it.
type Params struct {
	cmdbase.ParamsAgent
}

type DeviceNameParams struct {
	Params
	Name     string
	Username string `env:"FACTUM_DRIVER_USERNAME"`
	Password string `env:"FACTUM_DRIVER_PASSWORD"`
}

// ---------------------------------------------------------------------------

type deviceExecParams struct {
	DeviceNameParams
	Cmd string
}

func deviceExec() boa.CmdIfc {
	return boa.CmdT[deviceExecParams]{
		Use:   "exec",
		Short: "Exec command on device",
		RunFuncE: func(p *deviceExecParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}
			data, err := dm.Exec(p.Cmd)
			if err != nil {
				return err
			}
			util.Pprint(data)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------

func deviceVersion() boa.CmdIfc {
	return boa.CmdT[DeviceNameParams]{
		Use:   "version",
		Short: "Show device version",
		RunFuncE: func(p *DeviceNameParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}
			data, err := dm.Version()
			if err != nil {
				return err
			}
			util.Pprint(data)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
type deviceRunningConfigParams struct {
	DeviceNameParams
	Json bool `optional:"true"`
}

func deviceGetRunningConfig() boa.CmdIfc {
	return boa.CmdT[deviceRunningConfigParams]{
		Use:   "running-config-get",
		Short: "Get device running-config",
		RunFuncE: func(p *deviceRunningConfigParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}
			data, err := dm.RunningConfigGet(p.Json)
			if err != nil {
				return err
			}
			util.Pprint(data)
			return nil
		},
	}
}

func deviceSaveRunningConfig() boa.CmdIfc {
	return boa.CmdT[DeviceNameParams]{
		Use:   "running-config-save",
		Short: "Save running-config",
		RunFuncE: func(p *DeviceNameParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}
			err = dm.RunningConfigSave()
			if err != nil {
				return err
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------

func deviceInterfaceGetDescription() boa.CmdIfc {
	return boa.CmdT[DeviceNameParams]{
		Use:   "interfaces-get-status",
		Short: "Get interfaces status",
		RunFuncE: func(p *DeviceNameParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}
			data, err := dm.GetInterfacesStatus()
			if err != nil {
				return err
			}
			util.Pprint(data)
			return nil
		},
	}
}

type deviceSetInterfaceDescriptionParams struct {
	DeviceNameParams
	Ifname      string `descr:"Interface name" short:"i"`
	Description string `descr:"Interface description"`
}

func deviceSetInterfaceDescription() boa.CmdIfc {
	return boa.CmdT[deviceSetInterfaceDescriptionParams]{
		Use:   "set-interface-description",
		Short: "Set interface descriptions",
		RunFuncE: func(p *deviceSetInterfaceDescriptionParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}
			intf := netboxtool.NBInterface{
				Name:        p.Ifname,
				Description: p.Description,
			}
			err = dm.SetInterfaceDescription(&intf)
			if err != nil {
				return err
			}
			util.Pprint(err)
			return nil
		},
	}
}
func deviceGetConfig() boa.CmdIfc {
	return boa.CmdT[DeviceNameParams]{
		Use:   "get-all-config",
		Short: "Get all device config, parsed",
		RunFuncE: func(p *DeviceNameParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}

			dconf, err := dm.GetDeviceConfig()
			if err != nil {
				return err
			}
			util.Pprint(dconf)
			return nil
		},
	}
}

func deviceOpticalInventory() boa.CmdIfc {
	return boa.CmdT[DeviceNameParams]{
		Use:   "optical-inventory",
		Short: "Get Open ROADM optical inventory (ports, xconnects)",
		RunFuncE: func(p *DeviceNameParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}
			oc, ok := dm.(drivers.OpticalClient)
			if !ok {
				return errors.New("optical inventory is not supported on this platform")
			}
			inv, err := oc.GetOpticalInventory()
			if err != nil {
				return err
			}
			util.Pprint(inv)
			return nil
		},
	}
}

func deviceOpticalInventoryApply() boa.CmdIfc {
	return boa.CmdT[DeviceNameParams]{
		Use:   "optical-inventory-apply",
		Short: "Read Open ROADM inventory and persist it in Factum",
		RunFuncE: func(p *DeviceNameParams, cmd *cobra.Command, args []string) error {
			cmdbase.SetupLog(p.CommonParams)
			fc := factum.NewFactumClient(&p.Config.Factum)
			device, err := fc.GetDeviceByName(p.Name)
			if err != nil {
				return err
			}
			dm, err := drivers.NewDriverName(&p.Config.Factum, p.Name, p.Username, p.Password)
			if err != nil {
				return err
			}
			oc, ok := dm.(drivers.OpticalClient)
			if !ok {
				return errors.New("optical inventory is not supported on this platform")
			}
			inv, err := oc.GetOpticalInventory()
			if err != nil {
				return err
			}
			res, err := fc.ApplyOpticalInventory(device.ID, inv.ApplyInventory())
			if err != nil {
				return err
			}
			util.Pprint(res)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------

func main() {
	cmdbase.SetupCLI()
	cmdbase.Run(boa.CmdT[boa.NoParams]{
		Use:     "factum2-driver",
		Short:   "driver",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			deviceExec(),
			deviceVersion(),
			deviceGetRunningConfig(),
			deviceSaveRunningConfig(),
			deviceInterfaceGetDescription(),
			deviceSetInterfaceDescription(),
			deviceGetConfig(),
			deviceOpticalInventory(),
			deviceOpticalInventoryApply(),
			cmdbase.ShowConfigAgent(),
		),
	})
}
