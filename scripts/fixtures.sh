#!/usr/bin/env bash
# Re-captures testdata/soc/ from this machine's live mactop and scrubs it
# through cmd/wattop-scrub. Requires macOS with mactop installed and
# passwordless (see testdata/README.md). Does not touch testdata/agent/,
# which is hand-maintained.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

if ! command -v mactop >/dev/null 2>&1; then
	echo "fixtures.sh: mactop not found on PATH" >&2
	exit 1
fi

RAW="$(mktemp -d)"
trap 'rm -rf "$RAW"' EXIT
mkdir -p "$RAW/soc"

echo "fixtures.sh: capturing NDJSON framing (count 0, ~45s)..." >&2
# --count 0 never exits: give it a bounded window and take whatever landed.
# 45s comfortably clears the >=20-line acceptance bar at the default 1000ms
# interval, once mactop's own startup latency is accounted for.
timeout 45 mactop --headless --format json --count 0 --interval 1000 \
	>"$RAW/soc/mactop-ndjson.jsonl" || true

lines="$(wc -l <"$RAW/soc/mactop-ndjson.jsonl" | tr -d ' ')"
if [ "$lines" -lt 20 ]; then
	echo "fixtures.sh: only captured $lines lines of NDJSON, want >=20" >&2
	exit 1
fi

echo "fixtures.sh: capturing array framing (count 1)..." >&2
mactop --headless --format json --count 1 >"$RAW/soc/mactop-array.json"

echo "fixtures.sh: scrubbing into testdata/soc/..." >&2
go run ./cmd/wattop-scrub -src "$RAW/soc" -dst testdata/soc

echo "fixtures.sh: rebuilding testdata/soc/mactop-degraded.jsonl..." >&2
python3 - <<'PY'
import json

DROP_SOC_METRICS = {"ane_active", "dram_read_bw_gbs", "dram_write_bw_gbs", "dram_bw_combined_gbs"}

lines = []
with open("testdata/soc/mactop-ndjson.jsonl") as f:
    for line in f:
        rec = json.loads(line)
        sm = rec.get("soc_metrics", {})
        for k in DROP_SOC_METRICS:
            sm.pop(k, None)
        rec.pop("fans", None)
        lines.append(json.dumps(rec, separators=(",", ":")))

with open("testdata/soc/mactop-degraded.jsonl", "w") as f:
    f.write("\n".join(lines) + "\n")
PY

echo "fixtures.sh: done. $(wc -l <testdata/soc/mactop-ndjson.jsonl | tr -d ' ') NDJSON lines captured." >&2
