# Offline wheelhouse, step by step

For `python-pip-uv-venv`. Build the wheelhouse where there is network; install where there is none.

```bash
# 1. On the connected machine, inside the project env:
mkdir -p wheelhouse
pip download -r requirements.txt -d ./wheelhouse

# 2. Check the platform tags fit the target (manylinux, cp311, ...):
ls wheelhouse | head

# 3. Copy the whole project dir (requirements.txt + wheelhouse/) to the target.

# 4. On the target, create the env and install with no network:
python3 -m venv .venv && source .venv/bin/activate
pip install --no-index --find-links ./wheelhouse -r requirements.txt

# 5. Prove it:
pip check && python -c 'import <your_top_level_dep>; print("ok")'
```

Notes:

- Rebuild the wheelhouse when `requirements.txt` changes or the target Python/OS/arch differs.
- With `uv`: `uv pip install --no-index --find-links ./wheelhouse -r requirements.txt`.
- Keep `pip`/`setuptools`/`wheel` themselves in the wheelhouse or preinstalled on the target.
