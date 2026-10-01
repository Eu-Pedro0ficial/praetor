#!/usr/bin/env bash

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly REPO_ROOT="$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)"

readonly ARCH_DIR="$REPO_ROOT/docs/architecture"
readonly BOOTSTRAP_SCRIPT="$ARCH_DIR/scripts/bootstrap-docs.sh"
readonly BUILD_SCRIPT="$ARCH_DIR/scripts/build-docs.sh"
readonly OUTPUT_DIR="$REPO_ROOT/build/docs"
readonly OUTPUT_PDF="$OUTPUT_DIR/praetor-architecture.pdf"

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

info() {
    printf '==> %s\n' "$*"
}

command -v git >/dev/null 2>&1 ||
    die "git is required"

[[ -d "$ARCH_DIR" ]] ||
    die "architecture directory not found: $ARCH_DIR"

[[ -f "$ARCH_DIR/src/docs/arc42/arc42.adoc" ]] ||
    die "arc42 entrypoint not found"

[[ -f "$BOOTSTRAP_SCRIPT" ]] ||
    die "bootstrap script not found: $BOOTSTRAP_SCRIPT"

[[ -f "$BUILD_SCRIPT" ]] ||
    die "build script not found: $BUILD_SCRIPT"

# ---------------------------------------------------------------------------
# Bootstrap docToolchain when necessary
# ---------------------------------------------------------------------------

if [[ ! -x "$ARCH_DIR/dtcw" ]]; then
    info "docToolchain not found"
    info "Bootstrapping local documentation toolchain..."

    (
        cd "$ARCH_DIR"
        bash ./scripts/bootstrap-docs.sh
    )

    [[ -x "$ARCH_DIR/dtcw" ]] ||
        die "docToolchain bootstrap completed but dtcw was not created"
else
    info "docToolchain already available"
fi

# ---------------------------------------------------------------------------
# Generate documentation
# ---------------------------------------------------------------------------

info "Building Praetor architecture documentation..."

(
    cd "$ARCH_DIR"
    bash ./scripts/build-docs.sh
)

# ---------------------------------------------------------------------------
# Find generated PDF
# ---------------------------------------------------------------------------

mapfile -t pdfs < <(
    find "$ARCH_DIR" \
        -type f \
        -name '*.pdf' \
        -printf '%T@ %p\n' |
        sort -nr |
        cut -d' ' -f2-
)

if (( ${#pdfs[@]} == 0 )); then
    die "docToolchain completed but no generated PDF was found"
fi

SOURCE_PDF="${pdfs[0]}"

mkdir -p "$OUTPUT_DIR"
cp -- "$SOURCE_PDF" "$OUTPUT_PDF"

# ---------------------------------------------------------------------------
# Result
# ---------------------------------------------------------------------------

printf '\n'
info "Praetor architecture documentation generated successfully"
printf '\n'
printf 'PDF:  %s\n' "$OUTPUT_PDF"
printf 'Size: %s\n' "$(du -h "$OUTPUT_PDF" | cut -f1)"
printf '\n'
