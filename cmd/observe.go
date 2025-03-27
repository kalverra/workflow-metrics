package cmd

import (
	"fmt"

	"github.com/kalverra/workflow-metrics/observe"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	outputTypes []string
)

var observeCmd = &cobra.Command{
	Use:   "observe",
	Short: "Observe metrics from GitHub",
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Debug().
			Strs("output-types", outputTypes).
			Msg("observe flags")

		if workflowRunID != 0 {
			return observe.WorkflowRun(githubClient, owner, repo, workflowRunID, outputTypes)
		}

		if pullRequestID != "" {
			fmt.Errorf("pull request gathering not implemented yet")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(observeCmd)

	observeCmd.Flags().StringArrayVar(&outputTypes, "output-types", []string{"html", "md"}, "Output types to generate")
}
