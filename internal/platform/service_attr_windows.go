package platform

import "syscall"

var windowsSysProcAttr = syscall.SysProcAttr{HideWindow: true}
