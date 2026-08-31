package cmdbase

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/GiGurra/boa/pkg/boa"
	"github.com/abundo/factum2/internal/util"
	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

type CommonParams struct {
	Debug    bool   `short:"d" optional:"true" descr:"Enable verbose debug logging" `
	Loglevel string `short:"l" descr:"Set log level" alts:"error,warn,info,debug" default:"info" strict:"true"`
}
type ParamsAgent struct {
	ConfigFile string               `short:"f" configfile:"true" optional:"false" default:"/etc/factum2/factum2-worker.yaml"`
	Config     util.ConfigAgentRoot `yaml:",inline"`
	CommonParams
}
type Params struct {
	ConfigFile string          `short:"f" configfile:"true" optional:"false" default:"/etc/factum2/factum2.yaml"`
	Config     util.ConfigRoot `yaml:",inline"`
	CommonParams
}

func SetupCLI() {
	boa.RegisterConfigFormat(".yaml", yaml.Unmarshal)
}

func ShowConfig() boa.CmdIfc {
	return boa.CmdT[Params]{
		Use:   "show-config",
		Short: "Show configuration",
		RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
			SetupLog(p.CommonParams)
			util.Pprint(p.Config)
			return nil
		},
	}
}

// Migrate applies schema migrations (util.MigrateDatabase) and nothing else.
// Runtime commands connect with util.ConnectDatabase and must not call this
// as a side effect: AutoMigrate while factum2-web is serving will rewrite
// tables out from under the GUI. Stop the web process, run migrate, start
// it again. Uses Params (factum2.yaml with db:) even when the parent binary
// is otherwise a ParamsAgent API client.
func Migrate() boa.CmdIfc {
	return boa.CmdT[Params]{
		Use:   "migrate",
		Short: "Apply database schema migrations",
		Long:  "Apply schema migrations. Stop factum2-web first if it is running — do not migrate while the GUI holds the database.",
		RunFuncE: func(p *Params, cmd *cobra.Command, args []string) error {
			SetupLog(p.CommonParams)
			db, err := util.ConnectDatabase(&p.Config.DB)
			if err != nil {
				return err
			}
			if err := util.MigrateDatabase(db); err != nil {
				return err
			}
			fmt.Println("database migrations applied")
			return nil
		},
	}
}

// ShowConfigAgent is the show-config subcommand for binaries that embed
// ParamsAgent (factum/worker sections only) rather than the full Params -
// keeps show-config's required flags matching what the binary's other
// subcommands actually need.
//
// fetchRemote is optional: services that pull their database-backed settings
// from the primary over REST (dns, icinga, librenms, oxidized, prometheus) pass their
// FetchRemoteConfig, and its result is printed after the local config. A
// fetch failure is logged and the command still exits 0 - show-config should
// always report what it can, even with the primary unreachable, rather than
// have the whole command fail from a single missing section.
func ShowConfigAgent(fetchRemote ...func(*util.ConfigFactum) (any, error)) boa.CmdIfc {
	return boa.CmdT[ParamsAgent]{
		Use:   "show-config",
		Short: "Show configuration",
		RunFuncE: func(p *ParamsAgent, cmd *cobra.Command, args []string) error {
			SetupLog(p.CommonParams)
			util.Pprint(p.Config)
			if len(fetchRemote) > 0 {
				remote, err := fetchRemote[0](&p.Config.Factum)
				if err != nil {
					slog.Error("fetch remote config", "error", err)
					return nil
				}
				util.Pprint(remote)
			}
			return nil
		},
	}
}

func SetupLog(c CommonParams) error {
	level := c.Loglevel
	if c.Debug {
		level = "debug"
	}
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: logLevel == slog.LevelDebug,
	})
	slog.SetDefault(slog.New(handler))
	return nil
}
