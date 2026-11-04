//go:build darwin

package robotlab

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// These checks mock resource changes so they never lower the test process limits.
const darwinSandboxLimitsTest = `
import os
import sys
from unittest.mock import call, patch
sys.path.insert(0, os.getcwd())
from robotlab import sandbox
import resource

limit = (4 * 1024**3, 4 * 1024**3)
cases = [([], [call(resource.RLIMIT_AS, limit)]),
         ([ValueError('mapped'), None, None],
          [call(resource.RLIMIT_AS, limit), call(resource.RLIMIT_DATA, limit)]),
         ([OSError('mapped'), None, None],
          [call(resource.RLIMIT_AS, limit), call(resource.RLIMIT_DATA, limit)]),
         ([ValueError('mapped'), OSError('unsupported'), None],
          [call(resource.RLIMIT_AS, limit), call(resource.RLIMIT_DATA, limit)]),
         ([OSError('mapped'), ValueError('unsupported'), None],
          [call(resource.RLIMIT_AS, limit), call(resource.RLIMIT_DATA, limit)])]
for effects, expected in cases:
    for cpu, expected_cpu in [('0', 1), ('7200', 7200)]:
        with patch.object(sys, 'platform', 'darwin'), \
             patch.object(sys, 'argv', ['sandbox', '--cpu', cpu, '--module', 'probe', 'arg']), \
             patch.object(resource, 'setrlimit', side_effect=effects or None) as limits, \
             patch.object(os, 'getpriority', return_value=5), \
             patch.object(os, 'nice') as nice, \
             patch('runpy.run_module') as run:
            sandbox.main()
            assert limits.call_args_list == expected + [call(resource.RLIMIT_CPU, (expected_cpu, expected_cpu))]
            nice.assert_called_once_with(10)
            run.assert_called_once_with('probe', run_name='__main__')
            assert sys.argv == ['probe', 'arg']
print('Darwin sandbox memory fallback checks passed')
`

func TestDarwinSandboxMemoryLimits(t *testing.T) {
	python, _, _ := bootstrapPython()
	if _, err := exec.LookPath(python); err != nil {
		t.Skip("Python 3 is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-I", "-c", darwinSandboxLimitsTest)
	cmd.Dir = "runtime"
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sandbox resource limits: %v: %s", err, output)
	}
}
