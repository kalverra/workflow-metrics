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
)

const (
	timeoutDur = 10 * time.Second

	dataDir         = "data"
	workflowRunsDir = dataDir + string(filepath.Separator) + "workflow_runs"
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
		targetFile      = filepath.Join(workflowRunsDir, owner, repo, fmt.Sprintf("%d.json", workflowRunID))
		fileExists      = false
	)

	err := os.MkdirAll(workflowRunsDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to make data dir '%s': %w", workflowRunsDir, err)
	}

	if _, err := os.Stat(targetFile); err == nil {
		fileExists = true
	}

	if !forceUpdate && fileExists {
		workflowFileBytes, err := os.ReadFile(targetFile)
		if err != nil {
			return nil, fmt.Errorf("failed to open workflow run file: %w", err)
		}
		err = json.Unmarshal(workflowFileBytes, &workflowRunData)
		return workflowRunData, err
	}

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
