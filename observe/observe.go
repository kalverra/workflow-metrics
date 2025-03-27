package observe

import "time"

const (
	outputDir    = "observe_output"
	templatesDir = "observe/templates"
)

func determineDateFormat(start, end time.Time) (mermaidDateFormat, mermaidAxisFormat, goDateFormat string) {
	diff := end.Sub(start)
	if diff.Hours() > 24 {
		return "YYYY-MM-DD HH:mm:ss", "%Y-%m-%d %H:%M:%S", "2006-01-02 15:04:05"
	}

	if diff > time.Hour {
		return "HH:mm:ss", "%H:%M:%S", "15:04:05"
	}

	if diff > time.Minute {
		return "mm:ss", "%M:%S", "04:05"
	}

	return "ss.SS", "%S", "05.00"
}
