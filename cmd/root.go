package cmd

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/satisfactorymodding/ficsit-cli/cmd/installation"
	"github.com/satisfactorymodding/ficsit-cli/cmd/mod"
	"github.com/satisfactorymodding/ficsit-cli/cmd/profile"
	"github.com/satisfactorymodding/ficsit-cli/cmd/smr"
)

var RootCmd = &cobra.Command{
	Use:   "ficsit",
	Short: "cli mod manager for satisfactory",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		viper.SetConfigName("config")
		viper.AddConfigPath(".")
		viper.SetEnvPrefix("ficsit")
		viper.AutomaticEnv()

		_ = viper.ReadInConfig()

		level, err := zerolog.ParseLevel(viper.GetString("log"))
		if err != nil {
			panic(err)
		}

		zerolog.SetGlobalLevel(level)

		writers := make([]io.Writer, 0)
		if viper.GetBool("pretty") {
			pterm.EnableStyling()
		} else {
			pterm.DisableStyling()
		}

		if !viper.GetBool("quiet") {
			writers = append(writers, zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: time.RFC3339,
			})
		}

		if viper.GetString("log-file") != "" {
			logFile, err := os.OpenFile(viper.GetString("log-file"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o777)
			if err != nil {
				return errors.Wrap(err, "failed to open log file")
			}

			writers = append(writers, logFile)
		}

		log.Logger = zerolog.New(io.MultiWriter(writers...)).With().Timestamp().Logger()

		return nil
	},
}

func Execute(version string, commit string) {
	// Execute tea as default
	cmd, _, err := RootCmd.Find(os.Args[1:])

	// Allow opening via explorer
	cobra.MousetrapHelpText = ""

	cli := len(os.Args) >= 2 && os.Args[1] == "cli"
	if (len(os.Args) <= 1 || os.Args[1] != "help") && (err != nil || cmd == RootCmd) {
		args := append([]string{"cli"}, os.Args[1:]...)
		RootCmd.SetArgs(args)
		cli = true
	}

	// Always be quiet in CLI mode
	if cli {
		viper.Set("quiet", true)
	}

	viper.Set("version", version)
	viper.Set("commit", commit)

	if err := RootCmd.Execute(); err != nil {
		panic(err)
	}
}

func init() {
	RootCmd.AddCommand(cliCmd)
	RootCmd.AddCommand(applyCmd)
	RootCmd.AddCommand(versionCmd)
	RootCmd.AddCommand(searchCmd)
	RootCmd.AddCommand(profile.Cmd)
	RootCmd.AddCommand(installation.Cmd)
	RootCmd.AddCommand(mod.Cmd)
	RootCmd.AddCommand(smr.Cmd)

	var baseLocalDir string

	switch runtime.GOOS {
	case "windows":
		baseLocalDir = os.Getenv("APPDATA")
	case "linux":
		baseLocalDir = filepath.Join(os.Getenv("HOME"), ".local", "share")
	default:
		panic("unsupported platform: " + runtime.GOOS)
	}

	viper.Set("base-local-dir", baseLocalDir)

	baseCacheDir, err := os.UserCacheDir()
	if err != nil {
		panic(err)
	}

	RootCmd.PersistentFlags().String("log", "info", "The log level to output")
	RootCmd.PersistentFlags().String("log-file", "", "File to output logs to")
	RootCmd.PersistentFlags().Bool("quiet", false, "Do not log anything to console")
	RootCmd.PersistentFlags().Bool("pretty", true, "Whether to render pretty terminal output")

	RootCmd.PersistentFlags().Bool("dry-run", false, "Dry-run. Do not save any changes")

	RootCmd.PersistentFlags().String("cache-dir", filepath.Clean(filepath.Join(baseCacheDir, "ficsit")), "The cache directory")
	RootCmd.PersistentFlags().String("local-dir", filepath.Clean(filepath.Join(baseLocalDir, "ficsit")), "The local directory")
	RootCmd.PersistentFlags().String("profiles-file", "profiles.json", "The profiles file")
	RootCmd.PersistentFlags().String("installations-file", "installations.json", "The installations file")

	RootCmd.PersistentFlags().String("api-base", "https://api.ficsit.app", "URL for API")
	RootCmd.PersistentFlags().String("graphql-api", "/v2/query", "Path for GraphQL API")
	RootCmd.PersistentFlags().String("api-key", "", "API key to use when sending requests")

	_ = viper.BindPFlag("log", RootCmd.PersistentFlags().Lookup("log"))
	_ = viper.BindPFlag("log-file", RootCmd.PersistentFlags().Lookup("log-file"))
	_ = viper.BindPFlag("quiet", RootCmd.PersistentFlags().Lookup("quiet"))
	_ = viper.BindPFlag("pretty", RootCmd.PersistentFlags().Lookup("pretty"))

	_ = viper.BindPFlag("dry-run", RootCmd.PersistentFlags().Lookup("dry-run"))

	_ = viper.BindPFlag("cache-dir", RootCmd.PersistentFlags().Lookup("cache-dir"))
	_ = viper.BindPFlag("local-dir", RootCmd.PersistentFlags().Lookup("local-dir"))
	_ = viper.BindPFlag("profiles-file", RootCmd.PersistentFlags().Lookup("profiles-file"))
	_ = viper.BindPFlag("installations-file", RootCmd.PersistentFlags().Lookup("installations-file"))

	_ = viper.BindPFlag("api-base", RootCmd.PersistentFlags().Lookup("api-base"))
	_ = viper.BindPFlag("graphql-api", RootCmd.PersistentFlags().Lookup("graphql-api"))
	_ = viper.BindPFlag("api-key", RootCmd.PersistentFlags().Lookup("api-key"))
}
