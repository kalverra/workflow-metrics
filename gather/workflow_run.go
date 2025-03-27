package gather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-github/v70/github"
	"github.com/kalverra/workflow-metrics/monitor"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

const (
	timeoutDur = 10 * time.Second

	dataDir         = "data"
	workflowRunsDir = "workflow_runs"
)

var (
	ghCtx            = context.WithValue(context.Background(), github.SleepUntilPrimaryRateLimitResetWhenRateLimited, true)
	errGitHubTimeout = errors.New("github API timeout")

	// Mapping of how much a minute for each runner type costs
	// cost depicted in tenths of a cent
	// https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/about-billing-for-github-actions#per-minute-rates
	rateByRunner = map[string]int64{
		// https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/about-billing-for-github-actions#per-minute-rates-for-x64-powered-larger-runners
		"UBUNTU":         8,   // $0.008
		"UBUNTU_2_CORE":  8,   // $0.008
		"UBUNTU_4_CORE":  16,  // $0.016
		"UBUNTU_8_CORE":  32,  // $0.032
		"UBUNTU_16_CORE": 64,  // $0.064
		"UBUNTU_32_CORE": 128, // $0.128
		"UBUNTU_64_CORE": 256, // $0.256

		// https://docs.github.com/en/billing/managing-billing-for-your-products/managing-billing-for-github-actions/about-billing-for-github-actions#per-minute-rates-for-arm64-powered-larger-runners
		"UBUNTU_ARM":         5,   // $0.005
		"UBUNTU_2_CORE_ARM":  5,   // $0.005
		"UBUNTU_4_CORE_ARM":  10,  // $0.01
		"UBUNTU_8_CORE_ARM":  20,  // $0.02
		"UBUNTU_16_CORE_ARM": 40,  // $0.04
		"UBUNTU_32_CORE_ARM": 80,  // $0.08
		"UBUNTU_64_CORE_ARM": 160, // $0.16
	}
)

// JobsData wraps standard GitHub WorkflowJob data with additional cost fields
type JobsData struct {
	*github.WorkflowJob
	// Runner is the type of runner used for the job, e.g. "UBUNTU", "UBUNTU_2_CORE", "UBUNTU_4_CORE"
	Runner string `json:"runner"`
	// Cost is the cost of the job run in tenths of a cent
	Cost int64 `json:"cost"`
}

type WorkflowRunData struct {
	*github.WorkflowRun
	Jobs                []*JobsData           `json:"jobs,omitempty"`
	MonitorObservations *monitor.Observations `json:"monitor_observations,omitempty"`
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

	ctx, cancel := context.WithTimeoutCause(ghCtx, timeoutDur, errGitHubTimeout)
	workflowRun, _, err := client.Actions.GetWorkflowRunByID(ctx, owner, repo, workflowRunID)
	if err != nil {
		cancel()
		return nil, err
	}
	cancel()
	if workflowRun == nil {
		return nil, fmt.Errorf("workflow run '%d' not found on GitHub", workflowRunID)
	}
	if workflowRun.Status == nil || *workflowRun.Status != "completed" {
		return nil, fmt.Errorf("workflow run '%d' is still in progress", workflowRunID)
	}
	workflowRunData.WorkflowRun = workflowRun

	// TODO: Check for workflow-metrics artifact

	var (
		eg                  errgroup.Group
		workflowRunJobs     []*github.WorkflowJob
		workflowBillingData *github.WorkflowRunUsage
	)

	eg.Go(func() error {
		var jobsErr error
		workflowRunJobs, jobsErr = jobsData(client, owner, repo, workflowRunID)
		return jobsErr
	})

	eg.Go(func() error {
		var billingErr error
		workflowBillingData, billingErr = billingData(client, owner, repo, workflowRunID)
		return billingErr
	})

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("failed to collect job and/or billing data for workflow run '%d': %w", workflowRunID, err)
	}

	for _, job := range workflowRunJobs {
		runner, cost, err := calculateJobRunBilling(job.GetID(), workflowBillingData)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate cost for job '%d': %w", job.GetID(), err)
		}
		workflowRunData.Jobs = append(workflowRunData.Jobs, &JobsData{
			WorkflowJob: job,
			Runner:      runner,
			Cost:        cost,
		})
	}

	data, err := json.Marshal(workflowRunData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal workflow run data to json for workflow run '%d': %w", workflowRunID, err)
	}
	err = os.WriteFile(targetFile, data, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write workflow run data to file for workflow run '%d': %w", workflowRunID, err)
	}

	return workflowRunData, nil
}

func jobsData(client *github.Client, owner, repo string, workflowRunID int64) ([]*github.WorkflowJob, error) {
	var (
		workflowJobs = []*github.WorkflowJob{}
		listOpts     = &github.ListWorkflowJobsOptions{
			Filter: "all",
			ListOptions: github.ListOptions{
				PerPage: 100,
			},
		}
		resp *github.Response
	)

	startTime := time.Now()

	for { // Paginate through all jobs
		var (
			err  error
			jobs *github.Jobs
		)

		ctx, cancel := context.WithTimeoutCause(ghCtx, timeoutDur, errGitHubTimeout)
		jobs, resp, err = client.Actions.ListWorkflowJobs(ctx, owner, repo, workflowRunID, listOpts)
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
	log.Trace().
		Int("job_count", len(workflowJobs)).
		Str("duration", time.Since(startTime).String()).
		Int("api_calls_remaining", resp.Rate.Remaining).
		Str("rate_limit_reset", resp.Rate.Reset.String()).
		Str("owner", owner).
		Str("repo", repo).
		Int64("workflow_run_id", workflowRunID).
		Msg("Fetched jobs from GitHub")
	return workflowJobs, nil
}

func billingData(client *github.Client, owner, repo string, workflowRunID int64) (*github.WorkflowRunUsage, error) {
	startTime := time.Now()
	ctx, cancel := context.WithTimeoutCause(ghCtx, timeoutDur, errGitHubTimeout)
	usage, resp, err := client.Actions.GetWorkflowRunUsageByID(ctx, owner, repo, workflowRunID)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("failed to get billing data for workflow run '%d': %w", workflowRunID, err)
	}
	log.Trace().
		Str("duration", time.Since(startTime).String()).
		Int("api_calls_remaining", resp.Rate.Remaining).
		Str("rate_limit_reset", resp.Rate.Reset.String()).
		Str("owner", owner).
		Str("repo", repo).
		Int64("workflow_run_id", workflowRunID).
		Msg("Fetched billing data from GitHub")
	return usage, err
}

// calculateJobRunBilling calculates the cost of a job run based on the billing data
func calculateJobRunBilling(jobID int64, billingData *github.WorkflowRunUsage) (runner string, costInTenthsOfCents int64, err error) {
	if billingData == nil || billingData.GetBillable() == nil {
		return "", 0, fmt.Errorf("no billing data available")
	}
	for runner, billData := range *billingData.GetBillable() {
		if _, ok := rateByRunner[runner]; !ok {
			return "", 0, fmt.Errorf("no rate available for runner %s", runner)
		}
		for _, job := range billData.JobRuns {
			if int64(job.GetJobID()) == jobID {
				billableMinutes := job.GetDurationMS() / 1000 / 60
				costInTenthsOfCents = billableMinutes * rateByRunner[runner]
				return runner, costInTenthsOfCents, nil
			}
		}
	}
	// if we didn't find the job ID in billing data, it was free
	return "Free", 0, nil
}
