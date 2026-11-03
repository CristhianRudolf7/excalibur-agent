"""Child entry point: one experiment script executed for one configured run."""
from __future__ import annotations

import os
from pathlib import Path
import runpy
import sys
import time


def main() -> None:
    """Install run context and execute the experiment's single shared script."""
    directory = Path(sys.argv[1]).resolve()
    run_id = sys.argv[2]
    os.environ['EXCALIBUR_ROBOTLAB_EXPERIMENT_DIR'] = str(directory)
    os.environ['EXCALIBUR_ROBOTLAB_RUN_ID'] = run_id
    os.environ['EXCALIBUR_ROBOTLAB_RUN_STARTED'] = str(time.time())
    os.chdir(directory / 'runs' / run_id)
    runpy.run_path(str(directory / 'experiment.py'), run_name='__main__')


if __name__ == '__main__':
    main()
