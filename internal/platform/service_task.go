package platform

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// windowsDaemonCommand quotes argv using the Windows CommandLineToArgvW
// convention. Task Scheduler executes the image directly, without cmd.exe.
func windowsDaemonCommand(executable string) string {
	return quoteWindowsArg(executable) + " daemon start"
}

func quoteWindowsArg(arg string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range arg {
		if r == '\\' {
			slashes++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat("\\", slashes*2+1))
		} else {
			b.WriteString(strings.Repeat("\\", slashes))
		}
		slashes = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat("\\", slashes*2))
	b.WriteByte('"')
	return b.String()
}

// windowsTaskXML supplies the scheduler-only restart policy that /SC ONLOGON
// cannot express. Task Scheduler's minimum restart interval is one minute.
func windowsTaskXML(executable, sid string) string {
	escape := func(s string) string { var b strings.Builder; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
<Triggers><LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId></LogonTrigger></Triggers>
<Principals><Principal id="User"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>255</Count></RestartOnFailure></Settings>
<Actions Context="User"><Exec><Command>%s</Command><Arguments>daemon start</Arguments></Exec></Actions>
</Task>`, escape(sid), escape(sid), escape(executable))
}

func windowsTaskStatus(output string) string {
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "Status") {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "running":
				return "active"
			case "ready", "queued":
				return "inactive"
			case "disabled":
				return "disabled"
			}
		}
	}
	// Localized scheduler output still proves the task is installed.
	return "installed"
}
