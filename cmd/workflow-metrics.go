package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

const logTimeFormat = "2006-01-02T15:04:05.000"

// These variables are set at build time and describe the Version of the application
var (
	version = "dev"
	commit  = "dev"
	date    = time.Now().Format(time.RFC3339)
	builtBy = "local"
)

var (
	logFileName      string
	logLevelInput    string
	enableConsoleLog bool

	lumberLogger *lumberjack.Logger
)

var rootCmd = &cobra.Command{
	Use:   "workflow-metrics",
	Short: "",
	Long:  ``,
	RunE: func(cmd *cobra.Command, args []string) error {
		err := os.WriteFile(logFileName, []byte{}, 0644)
		if err != nil {
			return err
		}

		lumberLogger = &lumberjack.Logger{
			Filename:   logFileName,
			MaxSize:    100, // megabytes
			MaxBackups: 10,
			MaxAge:     30,
		}

		writers := []io.Writer{lumberLogger}
		if enableConsoleLog {
			writers = append(writers, os.Stdout)
		}

		logLevel, err := zerolog.ParseLevel(logLevelInput)
		if err != nil {
			return err
		}

		zerolog.TimeFieldFormat = logTimeFormat
		multiWriter := zerolog.MultiLevelWriter(writers...)
		log.Logger = zerolog.New(multiWriter).Level(logLevel).With().Timestamp().Logger()
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&logFileName, "log-file", "f", "workflow-metrics.log", "Log file name")
	rootCmd.PersistentFlags().StringVarP(&logLevelInput, "log-level", "l", "info", "Log level")
	rootCmd.PersistentFlags().BoolVarP(&enableConsoleLog, "enable-console-log", "c", false, "Enable console logs")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
