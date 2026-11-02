# Review

## What matters
A skill tells an agent what to do. A sentence open to two readings is a bug.
The letter and the skills must agree. A rule stated in the letter and contradicted in a skill is a bug.
No em dashes, no harness names in a skill. `scripts/verify.py` enforces both.

## Ignore
Vendored skills listed in vendor.json. They are upstream text.
decisions.md. It records what was true when each entry was written.

## High risk
install.sh
install.ps1
scripts/install-trufflehog.ps1
.github/workflows/

## Low risk
assets/
