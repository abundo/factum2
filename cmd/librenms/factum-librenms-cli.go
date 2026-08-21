package main

// ---------------------------------------------------------------------------
//
// This program can be run on librenms server
// Requires access to Factum API, librenms API and librenms mysql database
//
// ---------------------------------------------------------------------------

import (
	"os"

	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/internal/jobevent"
	"github.com/abundo/factum2/internal/librenms"
	"github.com/abundo/factum2/internal/util"
	"github.com/spf13/cobra"
)

type Params struct {
	cmdbase.ParamsAgent
}
type NameParams struct {
	Params
	Name string `short:"n" optional:"true"`
	ID   int    `short:"i" optional:"true"`
}
type SyncParams struct {
	Params
	Job bool `descr:"Emit structured job events (JSON lines) on stdout instead of human-readable output" optional:"true"`
}
type NormalizeHostnamesParams struct {
	Params
	Job bool `descr:"Emit structured job events (JSON lines) on stdout instead of human-readable output" optional:"true"`
}

func main() {
	cmdbase.SetupCLI()

	boa.CmdT[struct{}]{
		Use:     "factum-librenms",
		Short:   "Manage Librenms",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfigAgent(func(fc *util.ConfigFactum) (any, error) { return librenms.FetchRemoteConfig(fc) }),

			boa.CmdT[SyncParams]{
				Use:   "sync",
				Short: "Sync Librenms with Factum",
				RunFuncE: func(p *SyncParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					l, err := librenms.NewFactumLibrenmsClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					err = l.Sync(reporter)
					return err
				},
			},
			boa.CmdT[NormalizeHostnamesParams]{
				Use:   "normalize-hostnames",
				Short: "Set display name and rename hostname to IP for devices not keyed on an IP",
				RunFuncE: func(p *NormalizeHostnamesParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					l, err := librenms.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					var reporter jobevent.Reporter = jobevent.NewConsoleReporter(os.Stdout)
					if p.Job {
						reporter = jobevent.NewStdoutReporter(os.Stdout)
					}
					renamed, err := l.NormalizeHostnames(reporter)
					if err != nil {
						return err
					}
					reporter.Emit(jobevent.Info, "normalize-hostnames: renamed %d device(s)", renamed)
					return nil
				},
			},
			boa.CmdT[Params]{
				Use:   "get-devices",
				Short: "Get all librenms devices",
				RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					l, err := librenms.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					devices, err := l.DevicesGet()
					if err != nil {
						return err
					}
					util.Pprint(devices)
					return nil
				},
			},
			boa.CmdT[NameParams]{
				Use:   "get-device",
				Short: "Get one librenms device",
				RunFuncE: func(p *NameParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					l, err := librenms.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					var device *librenms.LibrenmsDevice
					if p.ID > 0 {
						device, err = l.DeviceGet(p.ID)
					} else {
						device, err = l.DeviceGetByName(p.Name)
					}
					if err != nil {
						return err
					}
					util.Pprint(device)
					return nil
				},
			},
			boa.CmdT[NameParams]{
				Use:   "get-device-ports",
				Short: "Get all ports for one librenms device",
				RunFuncE: func(p *NameParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					fl, err := librenms.NewFactumLibrenmsClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					l := fl.Librenms
					var ports []*librenms.LibrenmsPort
					if p.ID > 0 {
						ports, err = l.PortsGet(p.ID)
					} else {
						var device *librenms.LibrenmsDevice
						device, err = l.DeviceGetByName(p.Name)
						if err != nil {
							return err
						}
						ports, err = l.PortsGet(device.DeviceID)
					}
					if err != nil {
						return err
					}
					util.Pprint(ports)
					return nil
				},
			},
			boa.CmdT[NameParams]{
				Use:   "get-locations",
				Short: "Get all librenms locations",
				RunFuncE: func(p *NameParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					l, err := librenms.RemoteClient(&p.Config.Factum)
					if err != nil {
						return err
					}
					locations, err := l.LocationsGet()
					if err != nil {
						return err
					}
					util.Pprint(locations)
					return nil
				},
			},
		),
	}.Run()
}

// type Commands struct {
// 	CreateDevice     CreateDeviceCommand   `command:"create-device" description:"todo"`
// 	DeleteDevice     DeleteDeviceCommand   `command:"delete-device" description:"todo"`
// 	DeleteDeviceByID GetDeviceByIDCommand  `command:"delete-device-byid" description:"todo"`

// 	SetDeviceParent    SetDeviceParentCommand    `command:"set-device-parnet" description:"todo"`
// 	DeleteDeviceParent DeleteDeviceParentCommand `command:"delete-device-parent" description:"todo"`

// 	GetLocation            GetLocationCommand           `command:"get-location" description:"todo"`
// 	GetLocationByID        GetLocationByIDCommand       `command:"get-location-byid" description:"todo"`
// 	SetDevicecByIDLocation SetDeviceByIDLocationCommand `command:"set-device-location-byid" description:"todo"`
// }

// ---------------------------------------------------------------------------
//   Devices
// ---------------------------------------------------------------------------

/*

type CreateDeviceCommand struct {
	Name      string `long:"name" short:"n" required:"y"`
	ForceAdd  bool   `long:"forceadd" short:"f"`
	Version   string `long:"version" short:"v"`
	Community string `long:"community" short:"c"`
}

func (c *CreateDeviceCommand) Execute(args []string) error {
	var err error
	response, librenms := setup()
	response.Data, err = librenms.CreateDevice(c.Name, c.ForceAdd, c.Version, c.Community)
	if err != nil {
		response.Status = "error"
		response.Message = fmt.Sprintf("%s", err)
	}
	util.Pprint(response)
	return err
}

// ---------------------------------------------------------------------------
type DeleteDeviceCommand struct {
	Name string `long:"name" short:"n" required:"y"`
}

func (c *DeleteDeviceCommand) Execute(args []string) error {
	var err error
	response, librenms := setup()
	response.Data, err = librenms.DeleteDevice(c.Name)
	if err != nil {
		response.Status = "error"
		response.Message = fmt.Sprintf("%s", err)
	}
	util.Pprint(response)
	return err
}

// ---------------------------------------------------------------------------
type DeleteDeviceByIDCommand struct {
	ID int `long:"id" short:"i" required:"y"`
}

func (c *DeleteDeviceByIDCommand) Execute(args []string) error {
	var err error
	response, librenms := setup()

	response.Data, err = librenms.GetDeviceByID(c.ID)
	if err != nil {
		response.Status = "error"
		response.Message = fmt.Sprintf("%s", err)
	}
	util.Pprint(response)
	return err
}

// ---------------------------------------------------------------------------
//
//	Parents
//
// ---------------------------------------------------------------------------
type SetDeviceParentCommand struct {
	Name   string `long:"name" short:"n" required:"y"`
	Parent string `long:"parent" short:"p" required:"y"`
}

func (c *SetDeviceParentCommand) Execute(args []string) error {
	var err error
	// response, librenms := setup()

	// device = librenms_mgr.get_device(name=args.name)
	// data = librenms_mgr.set_device_parent(device_id=device["device_id"], parent=args.parent)
	// abutils.pprint(data)
	return err
}

// ---------------------------------------------------------------------------
type DeleteDeviceParentCommand struct {
	Name   string `long:"name" short:"n" required:"y"`
	Parent string `long:"parent" short:"p" required:"y"`
}

func (c *DeleteDeviceParentCommand) Execute(args []string) error {
	var err error

	response, librenms := setup()
	response.Data, err = librenms.GetDevice(c.Name)
	if err != nil {
		log.Fatalf("%s", err)
	}
	// device = librenms_mgr.get_device(name=args.name)
	// data = librenms_mgr.delete_device_parent(device_id=device["device_id"], parent=args.parent)
	// abutils.pprint(data)'
	util.Pprint(response)
	return err
}

// ---------------------------------------------------------------------------
//   Locations
// ---------------------------------------------------------------------------

type GetLocationCommand struct {
	Name string `long:"name" short:"n" required:"y"`
}

func (c *GetLocationCommand) Execute(args []string) error {
	var err error
	response, librenms := setup()
	response.Data, err = librenms.GetLocation(c.Name, true)
	if err != nil {
		response.Status = "error"
		response.Message = fmt.Sprintf("%s", err)
	}
	util.Pprint(response)
	return err
}

// ---------------------------------------------------------------------------
type GetLocationByIDCommand struct {
	ID int `long:"id" short:"i" required:"y"`
}

func (c *GetLocationByIDCommand) Execute(args []string) error {
	var err error
	response, librenms := setup()
	response.Data, err = librenms.GetLocationByID(c.ID, true)
	if err != nil {
		response.Status = "error"
		response.Message = fmt.Sprintf("%s", err)
	}

	util.Pprint(response)
	return err
}

// ---------------------------------------------------------------------------
type SetDeviceByIDLocationCommand struct {
	ID       int      `long:"id" short:"i" required:"y"`
	Location string   `long:"name" short:"n" required:"y"`
	Lat      *float64 `long:"lat" default:"999999"`
	Lng      *float64 `long:"lng" default:"999999"`
}

func (c *SetDeviceByIDLocationCommand) Execute(args []string) error {
	var err error
	response, librenms := setup()
	response.Data, err = librenms.SetDeviceByIDLocation(c.ID, c.Location, c.Lat, c.Lng)
	if err != nil {
		response.Status = "error"
		response.Message = fmt.Sprintf("%s", err)
	}
	util.Pprint(response)
	return err
}


*/
