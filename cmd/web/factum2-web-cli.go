// Web backend for factumn2
package main

// ---------------------------------------------------------------------------
//
// This program can be run primary server
//
// ---------------------------------------------------------------------------

import (
	"github.com/GiGurra/boa/pkg/boa"
	cmdbase "github.com/abundo/factum2/cmd"
	"github.com/abundo/factum2/internal/buildinfo"
	"github.com/abundo/factum2/web"
	"github.com/spf13/cobra"
)

func main() {
	cmdbase.SetupCLI()

	cmdbase.Run(boa.CmdT[boa.NoParams]{
		Use:     "factum2-web",
		Short:   "Manager factum WEB GUI",
		Version: buildinfo.Version,
		SubCmds: boa.SubCmds(
			cmdbase.ShowConfig(),
			cmdbase.Migrate(),

			boa.CmdT[web.GuiParams]{
				Use:   "start",
				Short: "Start factum WEB GUI",
				RunFuncE: func(p *web.GuiParams, cmd *cobra.Command, args []string) error {
					cmdbase.SetupLog(p.CommonParams)
					err := web.GUI(p)
					return err
				},
			},
			boa.CmdT[web.GuiParams]{
				Use:   "createadmin",
				Short: "(Re)create adminstrator",
				RunFuncE: func(p *web.GuiParams, cmd *cobra.Command, args []string) error {
					err := web.CreateAdmin(p)
					return err
				},
			}),
	})
}
