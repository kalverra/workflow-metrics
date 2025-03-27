package observe

import (
	"bytes"
	"fmt"
	htmlTemplate "html/template"
	"os"
	"path/filepath"
	"strings"
	textTemplate "text/template"
	"time"

	"github.com/google/go-github/v70/github"
	"github.com/kalverra/workflow-metrics/gather"
	"github.com/rs/zerolog/log"
)

func WorkflowRun(client *github.Client, owner, repo string, workflowRunID int64, outputTypes []string) error {
	outputDir := filepath.Join(outputDir, owner, repo)
	err := os.MkdirAll(outputDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	workflowRun, err := gather.WorkflowRun(client, owner, repo, workflowRunID, false)
	if err != nil {
		return err
	}

	var (
		startTime   = time.Now()
		outputFiles = make([]string, 0, len(outputTypes))
	)

	workflowRunTemplateData, err := buildWorkflowRunTemplateData(workflowRun)
	if err != nil {
		return fmt.Errorf("failed to generate mermaid chart: %w", err)
	}

	targetFile := filepath.Join(outputDir, fmt.Sprintf("workflow_run_%d", workflowRunID))
	for _, outputType := range outputTypes {
		var rendered string
		switch outputType {
		case "html":
			rendered, err = workflowRunRenderHTML(workflowRunTemplateData)
			if err != nil {
				return fmt.Errorf("failed to render HTML: %w", err)
			}
			err = os.WriteFile(targetFile+".html", []byte(rendered), 0644)
			if err != nil {
				return fmt.Errorf("failed to write HTML file: %w", err)
			}
		case "md":
			rendered = workflowRunRenderMarkdown(workflowRunTemplateData.MermaidChart)
		default:
			return fmt.Errorf("unknown output type '%s'", outputType)
		}

		finalFile := fmt.Sprintf("%s.%s", targetFile, outputType)
		err := os.WriteFile(finalFile, []byte(rendered), 0644)
		if err != nil {
			return fmt.Errorf("failed to write %s file: %w", outputType, err)
		}
		outputFiles = append(outputFiles, finalFile)
	}
	log.Info().
		Int64("workflow_run_id", workflowRunID).
		Strs("output_files", outputFiles).
		Str("duration", time.Since(startTime).String()).
		Msg("Observed workflow run")
	return nil
}

type workflowRunTemplateData struct {
	ID                int64
	MermaidDateFormat string
	MermaidAxisFormat string
	GoDateFormat      string
	Tasks             []mermaidTask
	MermaidChart      string
}

type mermaidTask struct {
	Name      string
	StartTime time.Time
	Duration  time.Duration
}

var mermaidTemplate = `gantt
    title Workflow Run {{ .ID }}
    dateFormat {{ .MermaidDateFormat }}
    axisFormat {{ .MermaidAxisFormat}}

    {{ $dateFormat := .GoDateFormat }}
    {{ range .Tasks }}
    {{ .Name }} :{{ .StartTime.Format $dateFormat }}, {{ .Duration.Seconds }}s{{ end }}`

func buildWorkflowRunTemplateData(workflowRun *gather.WorkflowRunData) (*workflowRunTemplateData, error) {
	mermaidDateFormat, mermaidAxisFormat, goDateFormat := determineDateFormat(
		workflowRun.GetRunStartedAt().Time,
		workflowRun.GetUpdatedAt().Time, // TODO: UpdatedAt is probably inaccurate
	)

	tasks := make([]mermaidTask, 0, len(workflowRun.Jobs))
	for _, job := range workflowRun.Jobs {
		startedAt := job.GetStartedAt().Time
		duration := job.GetCompletedAt().Sub(startedAt)
		if startedAt.IsZero() || duration == 0 {
			continue
		}

		jobName := job.GetName()
		// Colons in names break mermaid rendering https://github.com/mermaid-js/mermaid/issues/742
		jobName = strings.ReplaceAll(jobName, ":", "#colon;")
		tasks = append(tasks, mermaidTask{
			Name:      jobName,
			StartTime: job.GetStartedAt().Time,
			Duration:  job.GetCompletedAt().Sub(job.GetStartedAt().Time),
		})
	}

	var mermaidChart bytes.Buffer
	templateData := &workflowRunTemplateData{
		ID:                workflowRun.GetID(),
		MermaidDateFormat: mermaidDateFormat,
		MermaidAxisFormat: mermaidAxisFormat,
		GoDateFormat:      goDateFormat,
		Tasks:             tasks,
	}

	tmpl, err := textTemplate.New("mermaid").Parse(mermaidTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse mermaid template: %w", err)
	}

	err = tmpl.Execute(&mermaidChart, templateData)
	if err != nil {
		return nil, fmt.Errorf("failed to execute mermaid template: %w", err)
	}

	templateData.MermaidChart = mermaidChart.String()
	return templateData, nil
}

func workflowRunRenderHTML(templateData *workflowRunTemplateData) (string, error) {
	tmpl, err := htmlTemplate.New("workflow_run").ParseFiles(filepath.Join(templatesDir, "workflow_run.html"))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML template: %w", err)
	}

	var html bytes.Buffer
	err = tmpl.Execute(&html, templateData)
	if err != nil {
		return "", fmt.Errorf("failed to execute HTML template: %w", err)
	}
	return html.String(), nil
}

func workflowRunRenderMarkdown(mermaidChart string) string {
	return fmt.Sprintf("```mermaid\n%s\n```", mermaidChart)
}
