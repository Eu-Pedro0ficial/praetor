#!/usr/bin/env bash
set -euo pipefail

# Official docToolchain wrapper. At architecture-baseline time the wrapper defaults
# to the current stable docToolchain 3.5.0 release. Override DTC_VERSION explicitly
# if the project later pins a different reviewed release.
if [[ ! -f ./dtcw ]]; then
  curl -fsSL https://doctoolchain.github.io/dtcw -o ./dtcw
  chmod +x ./dtcw
fi

./dtcw local install doctoolchain

echo "docToolchain installed. Generate docs with:"
echo "  ./dtcw generateHTML generatePDF"
