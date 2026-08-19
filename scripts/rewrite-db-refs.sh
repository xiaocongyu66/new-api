#!/bin/bash
# Rewrite `DB.` to `model.DB.` in domain model packages and ensure `model` is imported.
# Excludes identity/model/auth_flow.go (already done manually).
set -e
cd /home/hathaway/projects/new-api/.wt/phase0

domains="apps/api/internal/catalog/model apps/api/internal/identity/model apps/api/internal/billing/model apps/api/internal/usage/model"

for dir in $domains; do
  for f in "$dir"/*.go; do
    [ -f "$f" ] || continue
    # Skip files that already reference model.DB or have explicit model import with DB ref
    if grep -qE '\bmodel\.DB\b' "$f"; then
      continue
    fi
    # Replace `DB.` (not preceded by `.` or alphanum) with `model.DB.`
    # Use perl for negative lookbehind
    perl -i -pe 's/(?<![\w.])DB\./model.DB./g; s/(?<![\w.])DB\b(?![\w.])/model.DB/g' "$f"
    # Ensure "github.com/QuantumNous/new-api/model" is imported
    if grep -qE '\bmodel\.DB\b' "$f" && ! grep -qE '"github\.com/QuantumNous/new-api/model"' "$f"; then
      # Insert into imports block
      python3 - "$f" <<'PYEOF'
import sys, re
p = sys.argv[1]
src = open(p).read()
if '"github.com/QuantumNous/new-api/model"' in src:
    sys.exit(0)
m = re.search(r'(import \()', src)
if m:
    # insert before the close paren
    idx = src.find(')', m.end())
    src = src[:idx].rstrip() + '\n\t"github.com/QuantumNous/new-api/model"\n' + src[idx:]
else:
    # single import
    m = re.search(r'(import "[^"]+")', src)
    if m:
        # find the end of imports
        idx = src.find('"', m.end())
        # already has multiple? group all imports into a block
        # naive: just add a new import line below the existing one
        # actually it's simpler to just print what we have
        print("WARN: complex import", p)
        sys.exit(1)
open(p, 'w').write(src)
PYEOF
    fi
  done
done
echo "done"
