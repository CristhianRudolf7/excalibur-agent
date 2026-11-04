package daemon

// SendNotify is a no-op: Task Scheduler does not use sd_notify.
func SendNotify(socketPath, message string) error { return nil }
