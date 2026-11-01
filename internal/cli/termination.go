package cli

import "errors"

// ErrTermination distinguishes an external timeout signal from a user interrupt.
var ErrTermination = errors.New("external task timeout (SIGTERM)")
