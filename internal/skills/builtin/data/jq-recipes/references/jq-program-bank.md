# jq program bank

For `jq-recipes`. Test each filter on a sample before streaming it over the full file.

```bash
# Keys of the first object (schema discovery)
jq '.[0] | keys' data.json

# Failed items with id and message
jq '.items[] | select(.status == "failed") | {id, msg: .error}' run.json

# Count by a field
jq -s 'group_by(.level) | map({level: .[0].level, n: length}) | sort_by(-.n)' app.jsonl

# Flatten to CSV with header
jq -r '["id","status","ms"], (.items[] | [.id, .status, (.ms // 0)]) | @csv' run.json

# Sum a numeric field, ignoring missing
jq '[.items[] | .bytes // 0] | add' run.json

# Distinct sorted values of a field
jq -r '.events[] | .tool' calls.jsonl | sort -u

# Keep only these keys, recursively redacting secrets
jq 'walk(if type == "object" then with_entries(select(.key | test("key|secret|token|password") | not)) else . end)' cfg.json

# Assert a condition (exit 1 when false)
jq -e '.gate.attempts | length > 0' final.json

# Join two files on id (small right side)
jq -s '.[0].users as $u | .[1].events[] | . + {name: $u[.uid]}' users.json events.json

# Timestamps: newest first (ISO 8601 strings sort lexicographically)
jq -s 'sort_by(.ts) | reverse | .[0:5]' events.jsonl
```
