package cmd

import (
	"fmt"
	"os"

	"github.com/gofri/go-github-ratelimit/github_ratelimit"
	"github.com/google/go-github/v70/github"
	"github.com/kalverra/workflow-metrics/gather"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

const githubTokenEnvVar = "GITHUB_TOKEN"

var (
	owner string
	repo  string

	workflowRunID int64
	pullRequestID string
	githubToken   string
	forceUpdate   bool
)

var gatherCmd = &cobra.Command{
	Use:   "gather",
	Short: "Gather metrics from GitHub",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if workflowRunID == 0 && pullRequestID == "" {
			return fmt.Errorf("either workflow run ID or pull request ID must be provided")
		}
		if workflowRunID != 0 && pullRequestID != "" {
			return fmt.Errorf("only one of workflow run ID or pull request ID must be provided")
		}
		if githubToken != "" {
			log.Debug().Msg("Using GitHub token from flag")
		} else if os.Getenv(githubTokenEnvVar) != "" {
			log.Debug().Msg("Using GitHub token from environment variable")
		} else {
			log.Warn().Msg("GitHub token not provided, will likely hit rate limits quickly")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		rateLimiter, err := github_ratelimit.NewRateLimitWaiterClient(nil)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to create rate limiter")
		}
		client := github.NewClient(rateLimiter)
		if githubToken != "" {
			client = client.WithAuthToken(githubToken)
		}

		if workflowRunID != 0 {
			_, err := gather.WorkflowRun(client, owner, repo, workflowRunID, forceUpdate)
			return err
		}

		if pullRequestID != "" {
			return fmt.Errorf("pull request gathering not implemented yet")
		}

		return nil
	},
}

func init() {
	gatherCmd.Flags().StringVarP(&owner, "owner", "o", "", "Repository owner")
	gatherCmd.Flags().StringVarP(&repo, "repo", "r", "", "Repository name")
	gatherCmd.Flags().Int64VarP(&workflowRunID, "workflow-run-id", "w", 0, "Workflow run ID")
	gatherCmd.Flags().StringVarP(&pullRequestID, "pull-request-id", "p", "", "Pull request ID")
	gatherCmd.Flags().StringVarP(&githubToken, "github-token", "t", "", fmt.Sprintf("GitHub API token (can also be set via %s)", githubTokenEnvVar))
	gatherCmd.Flags().BoolVarP(&forceUpdate, "force-update", "u", false, "Force update of existing data")

	gatherCmd.MarkFlagRequired("owner")
	gatherCmd.MarkFlagRequired("repo")

	rootCmd.AddCommand(gatherCmd)
}
