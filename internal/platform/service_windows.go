package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const windowsTaskName = "Excalibur Daemon"
const windowsRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// WindowsServiceManager manages a limited per-user logon task with restart on
// failure. Systems without schtasks.exe use the current user's Run key.
type WindowsServiceManager struct {
	ExecPath string
	TaskName string
}

var _ ServiceManager = WindowsServiceManager{}

func (m WindowsServiceManager) taskName() string {
	if m.TaskName != "" {
		return m.TaskName
	}
	return windowsTaskName
}
func (m WindowsServiceManager) execPath() (string, error) {
	if m.ExecPath != "" {
		return m.ExecPath, nil
	}
	if path := os.Getenv("EXCALIBUR_EXEC_PATH"); path != "" {
		return path, nil
	}
	return os.Executable()
}
func schedulerRun(path string, args ...string) (string, error) {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &windowsSysProcAttr
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("platform: schtasks: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
func (m WindowsServiceManager) Install() error {
	executable, err := m.execPath()
	if err != nil {
		return err
	}
	scheduler, err := exec.LookPath("schtasks.exe")
	if errors.Is(err, exec.ErrNotFound) {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		return key.SetStringValue(m.taskName(), windowsDaemonCommand(executable))
	}
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "excalibur-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := RestrictToOwner(file.Name()); err != nil {
		_ = file.Close()
		return err
	}
	_, err = file.Write(windowsTaskFile(executable, user.User.Sid.String()))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err := schedulerRun(scheduler, "/Create", "/TN", m.taskName(), "/XML", file.Name(), "/F"); err != nil {
		return err
	}
	_, err = schedulerRun(scheduler, "/Run", "/TN", m.taskName())
	return err
}
func (m WindowsServiceManager) Uninstall() error {
	// Remove either backend, including a Run key left from an older install.
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE)
	if err == nil {
		err = key.DeleteValue(m.taskName())
		_ = key.Close()
	}
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	scheduler, err := exec.LookPath("schtasks.exe")
	if errors.Is(err, exec.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	exists, err := m.taskExists(scheduler)
	if err != nil || !exists {
		return err
	}
	_, _ = schedulerRun(scheduler, "/End", "/TN", m.taskName())
	_, err = schedulerRun(scheduler, "/Delete", "/TN", m.taskName(), "/F")
	return err
}
func (m WindowsServiceManager) taskExists(scheduler string) (bool, error) {
	// The task name is stable across OS languages; error messages are not.
	out, err := schedulerRun(scheduler, "/Query", "/FO", "CSV", "/NH")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), ",")
		if strings.EqualFold(strings.Trim(name, "\"\\"), m.taskName()) {
			return true, nil
		}
	}
	return false, nil
}
func (m WindowsServiceManager) Status() (string, error) {
	scheduler, err := exec.LookPath("schtasks.exe")
	if err == nil {
		exists, err := m.taskExists(scheduler)
		if err != nil {
			return "", err
		}
		if exists {
			out, err := schedulerRun(scheduler, "/Query", "/TN", m.taskName(), "/FO", "LIST", "/V")
			if err != nil {
				return "", err
			}
			return windowsTaskStatus(out), nil
		}
	} else if !errors.Is(err, exec.ErrNotFound) {
		return "", err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	defer key.Close()
	_, _, err = key.GetStringValue(m.taskName())
	if errors.Is(err, registry.ErrNotExist) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	return "installed", nil
}
