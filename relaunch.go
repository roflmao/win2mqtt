package main

import "time"

// relaunchCommandLine builds the cmd.exe command that re-runs the Scheduled Task
// after a short delay. ping is used as the delay because timeout.exe exits
// immediately when stdin is not a console, which is always the case here.
func relaunchCommandLine(taskName string) string {
	return "ping -n 4 127.0.0.1 >nul & schtasks /Run /TN " + taskName
}

// bootTimeFromUptime converts a millisecond uptime into a boot timestamp with
// whole-second precision.
func bootTimeFromUptime(now time.Time, uptimeMs uint64) time.Time {
	return now.Add(-time.Duration(uptimeMs) * time.Millisecond).Truncate(time.Second)
}
