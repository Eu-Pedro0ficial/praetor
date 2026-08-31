#!/usr/bin/env bash
set -euo pipefail

if [[ ! -x ./dtcw ]]; then
  echo "dtcw not found. Run ./scripts/bootstrap-docs.sh first." >&2
  exit 1
fi

./dtcw generateHTML generatePDF
