package devcleanup

import (
	"os/exec"
	"runtime"
	"strings"
)

func runningProcesses() map[string]struct{} {
	out := make(map[string]struct{})
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tasklist", "/FO", "CSV", "/NH")
	} else {
		cmd = exec.Command("ps", "-A", "-o", "comm=")
	}
	data, err := cmd.Output()
	if err != nil {
		return out
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			// CSV starts with image name, e.g. "Code.exe","1234",...
			if strings.HasPrefix(line, "\"") {
				line = strings.TrimPrefix(line, "\"")
				parts := strings.SplitN(line, "\"", 2)
				if len(parts) > 0 {
					line = parts[0]
				}
			}
		}
		out[strings.ToLower(line)] = struct{}{}
	}
	return out
}

func activeProcessForTask(task CleanupTask, processList map[string]struct{}) string {
	for _, hint := range task.ProcessHints {
		hint = strings.ToLower(strings.TrimSpace(hint))
		if hint == "" {
			continue
		}
		for process := range processList {
			if strings.Contains(process, hint) {
				return hint
			}
		}
	}
	return ""
}
