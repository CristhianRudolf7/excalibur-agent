# Pipeline, find and xargs recipes

Copy-paste patterns for `shell-power-use`. Every recipe is one line; adapt paths and patterns.

## Pipelines

```bash
# Frequency table of values in column 2 of a CSV
cut -d, -f2 data.csv | sort | uniq -c | sort -rn | head -20

# Total size of *.log files, human readable
find /var/log -name '*.log' -type f -printf '%s %p\n' | awk '{s+=$1} END {print s/1024/1024 " MB"}'

# Newest 10 files under a tree
find . -type f -printf '%T@ %p\n' | sort -rn | head -10 | cut -d' ' -f2-

# Errors with 5 lines of context, deduplicated
grep -rn 'ERROR' --include='*.log' . | sort -u | head -50

# Kill all processes matching a pattern (careful: check first with pgrep)
pgrep -af 'my-worker --shard' | awk '{print $1}' | xargs -r kill
```

## find

```bash
find <dir> -name '<glob>' -type f   # by name; quote the glob
find <dir> -mtime -1 -type f        # modified in the last day
find <dir> -size +100M -type f      # large files
find <dir> -name '*.tmp' -delete    # delete matches (safe: -print first)
find <dir> -type f -exec grep -l 'pattern' {} +  # act on matches in batches
```

## xargs

```bash
find . -name '*.bak' -print0 | xargs -0 -r rm   # null-safe deletes
printf '%s\n' host1 host2 | xargs -P4 -I{} ssh {} uptime  # parallel ssh
ls *.csv | xargs -n1 -P"$(nproc)" python process.py      # parallel jobs
```
