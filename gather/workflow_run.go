package gather

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-github/v70/github"
	"github.com/kalverra/workflow-metrics/monitor"
	"github.com/rs/zerolog/log"
)

const (
	timeoutDur = 10 * time.Second

	dataDir         = "data"
	workflowRunsDir = "workflow_runs"
)

type WorkflowRunData struct {
	*github.WorkflowRun
	Jobs        []*github.WorkflowJob
	MonitorInfo *monitor.Observations
}

// PR Run: https://github.com/smartcontractkit/chainlink/actions/runs/14093870542
// Merge Group Run: https://github.com/smartcontractkit/chainlink/actions/runs/14093996551

// WorkflowRun gathers all metrics for a completed workflow run
func WorkflowRun(client *github.Client, owner, repo string, workflowRunID int64, forceUpdate bool) (*WorkflowRunData, error) {
	var (
		workflowRunData = &WorkflowRunData{}
		targetDir       = filepath.Join(dataDir, owner, repo, workflowRunsDir)
		targetFile      = filepath.Join(targetDir, fmt.Sprintf("%d.json", workflowRunID))
		fileExists      = false
	)

	err := os.MkdirAll(targetDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to make data dir '%s': %w", workflowRunsDir, err)
	}

	if _, err := os.Stat(targetFile); err == nil {
		fileExists = true
	}

	startTime := time.Now()
	log.Info().Int64("workflow_run_id", workflowRunID).Msg("Gathering workflow run data")
	defer func() {
		log.Info().
			Str("duration", time.Since(startTime).String()).
			Int64("workflow_run_id", workflowRunID).
			Msg("Gathered workflow run data")
	}()

	if !forceUpdate && fileExists {
		log.Debug().Str("file", targetFile).Int64("workflow_run_id", workflowRunID).Msg("Reading workflow run data from file")
		workflowFileBytes, err := os.ReadFile(targetFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open workflow run file: %w", err)
		}
		err = json.Unmarshal(workflowFileBytes, &workflowRunData)
		return workflowRunData, err
	}

	log.Debug().Int64("workflow_run_id", workflowRunID).Msg("Fetching workflow run data from GitHub")

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDur)
	workflowRun, _, err := client.Actions.GetWorkflowRunByID(ctx, owner, repo, workflowRunID)
	if err != nil {
		cancel()
		return nil, err
	}
	cancel()
	if workflowRun == nil {
		return nil, fmt.Errorf("workflow run '%d' not found", workflowRunID)
	}
	if workflowRun.Status == nil || *workflowRun.Status != "completed" {
		return nil, fmt.Errorf("workflow run '%d' is in progress", workflowRunID)
	}
	workflowRunData.WorkflowRun = workflowRun

	// TODO: Check for workflow-metrics artifact

	var (
		workflowJobs = []*github.WorkflowJob{}
		listOpts     = &github.ListWorkflowJobsOptions{
			Filter: "all",
			ListOptions: github.ListOptions{
				PerPage: 100,
			},
		}
	)

	for { // Paginate through all jobs
		ctx, cancel := context.WithTimeout(context.Background(), timeoutDur)
		jobs, resp, err := client.Actions.ListWorkflowJobs(ctx, owner, repo, workflowRunID, listOpts)
		if err != nil {
			cancel()
			return nil, err
		}
		cancel()
		workflowJobs = append(workflowJobs, jobs.Jobs...)
		if resp.NextPage == 0 {
			break
		}
		listOpts.Page = resp.NextPage
	}
	workflowRunData.Jobs = workflowJobs

	// TODO: Include runner and costs data

	data, err := json.Marshal(workflowRunData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal workflow run data to json: %w", err)
	}
	err = os.WriteFile(targetFile, data, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write workflow run data to file: %w", err)
	}

	return workflowRunData, nil
}
