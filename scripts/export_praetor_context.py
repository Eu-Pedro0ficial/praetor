#!/usr/bin/env python3

from __future__ import annotations

import argparse
import datetime as dt
import subprocess
import sys
from pathlib import Path


DEFAULT_OUTPUT = "praetor-project-context.txt"
DEFAULT_MAX_FILE_BYTES = 512 * 1024

# Código externo/dependências vendorizadas não fazem parte do contexto
# autoral do Praetor. Submodules continuam registrados separadamente.
EXCLUDED_CONTEXT_ROOTS = {
    "vendor",
}


def run_git(root: Path, *args: str, check: bool = True) -> str:
    result = subprocess.run(
        ["git", "-C", str(root), *args],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )

    if check and result.returncode != 0:
        raise RuntimeError(
            f"git {' '.join(args)} falhou:\n{result.stderr.strip()}"
        )

    return result.stdout.strip()


def find_git_root(start: Path) -> Path:
    result = subprocess.run(
        ["git", "-C", str(start), "rev-parse", "--show-toplevel"],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )

    if result.returncode != 0:
        raise RuntimeError(
            "O diretório informado não pertence a um repositório Git."
        )

    return Path(result.stdout.strip()).resolve()


def git_files(root: Path) -> list[Path]:
    """
    Retorna:
    - arquivos versionados;
    - arquivos não versionados;
    - exclui tudo respeitado por .gitignore/.git/info/exclude/global excludes.

    Submodules aparecem como uma única entrada e não são expandidos.
    """
    result = subprocess.run(
        [
            "git",
            "-C",
            str(root),
            "ls-files",
            "--cached",
            "--others",
            "--exclude-standard",
            "-z",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )

    if result.returncode != 0:
        raise RuntimeError(result.stderr.decode(errors="replace"))

    entries = result.stdout.decode(errors="surrogateescape").split("\0")

    return sorted(
        (Path(entry) for entry in entries if entry),
        key=lambda path: str(path).lower(),
    )


def filter_context_files(files: list[Path]) -> list[Path]:
    """
    Remove apenas raízes que não pertencem ao código autoral do Praetor.

    Importante:
    - não remove código, testes ou documentação do projeto;
    - dependências vendorizadas ficam fora do snapshot;
    - submodules são representados separadamente;
    - .gitignore continua sendo respeitado por git_files().
    """
    return [
        path
        for path in files
        if not path.parts or path.parts[0] not in EXCLUDED_CONTEXT_ROOTS
    ]


def git_submodules(root: Path) -> dict[str, str]:
    """Retorna {path: commit} para os submodules conhecidos pelo Git."""
    output = run_git(root, "submodule", "status", "--recursive", check=False)

    submodules: dict[str, str] = {}

    if not output:
        return submodules

    for line in output.splitlines():
        line = line.strip()

        if not line:
            continue

        marker = line[0]

        if marker in {"-", "+", "U", " "}:
            line = line[1:].strip()

        parts = line.split()

        if len(parts) >= 2:
            commit = parts[0]
            path = parts[1]
            submodules[path] = commit

    return submodules


def is_probably_binary(path: Path) -> bool:
    try:
        with path.open("rb") as file:
            chunk = file.read(8192)
    except OSError:
        return True

    if not chunk:
        return False

    if b"\x00" in chunk:
        return True

    try:
        chunk.decode("utf-8")
        return False
    except UnicodeDecodeError:
        return True


def format_size(size: int) -> str:
    units = ["B", "KiB", "MiB", "GiB"]
    value = float(size)

    for unit in units:
        if value < 1024 or unit == units[-1]:
            if unit == "B":
                return f"{int(value)} {unit}"
            return f"{value:.1f} {unit}"

        value /= 1024

    return f"{size} B"


def build_tree(files: list[Path], submodules: dict[str, str]) -> str:
    tree: dict = {}

    for path in files:
        parts = path.parts
        current = tree

        for index, part in enumerate(parts):
            last = index == len(parts) - 1

            if last:
                current.setdefault("__files__", []).append(part)
            else:
                current = current.setdefault(part, {})

    for submodule_path in submodules:
        parts = Path(submodule_path).parts
        current = tree

        for index, part in enumerate(parts):
            last = index == len(parts) - 1

            if last:
                current.setdefault("__submodules__", []).append(part)
            else:
                current = current.setdefault(part, {})

    lines: list[str] = ["."]

    def render(node: dict, prefix: str = "") -> None:
        directories = sorted(
            key
            for key in node.keys()
            if key not in {"__files__", "__submodules__"}
        )
        files_here = sorted(node.get("__files__", []))
        submodules_here = sorted(node.get("__submodules__", []))

        items: list[tuple[str, str]] = []
        items.extend(("dir", name) for name in directories)
        items.extend(("submodule", name) for name in submodules_here)
        items.extend(("file", name) for name in files_here)

        for index, (kind, name) in enumerate(items):
            is_last = index == len(items) - 1
            branch = "└── " if is_last else "├── "
            continuation = "    " if is_last else "│   "

            if kind == "dir":
                lines.append(f"{prefix}{branch}{name}/")
                render(node[name], prefix + continuation)
            elif kind == "submodule":
                full_path = _find_submodule_path(submodules, name)
                commit = submodules.get(full_path, "")
                suffix = (
                    f" [submodule @ {commit[:12]}]"
                    if commit
                    else " [submodule]"
                )
                lines.append(f"{prefix}{branch}{name}/{suffix}")
            else:
                lines.append(f"{prefix}{branch}{name}")

    render(tree)
    return "\n".join(lines)


def _find_submodule_path(
    submodules: dict[str, str],
    basename: str,
) -> str:
    candidates = [
        path
        for path in submodules
        if Path(path).name == basename
    ]

    if len(candidates) == 1:
        return candidates[0]

    return candidates[0] if candidates else basename


def language_hint(path: Path) -> str:
    suffix = path.suffix.lower()

    mapping = {
        ".go": "go",
        ".mod": "gomod",
        ".sum": "gosum",
        ".py": "python",
        ".adoc": "asciidoc",
        ".puml": "plantuml",
        ".md": "markdown",
        ".json": "json",
        ".yaml": "yaml",
        ".yml": "yaml",
        ".toml": "toml",
        ".ini": "ini",
        ".xml": "xml",
        ".html": "html",
        ".css": "css",
        ".js": "javascript",
        ".ts": "typescript",
        ".sh": "bash",
        ".groovy": "groovy",
        ".sql": "sql",
        ".txt": "text",
    }

    if path.name == "Makefile":
        return "make"
    if path.name == "Dockerfile":
        return "dockerfile"
    if path.name == ".gitignore":
        return "gitignore"
    if path.name == ".gitattributes":
        return "gitattributes"

    return mapping.get(suffix, "text")


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8", errors="replace")


def append_header(lines: list[str], title: str) -> None:
    lines.extend(
        [
            "",
            "=" * 88,
            title,
            "=" * 88,
            "",
        ]
    )


def export_context(
    root: Path,
    output: Path,
    max_file_bytes: int,
) -> None:
    files = filter_context_files(git_files(root))
    submodules = git_submodules(root)

    output_relative: Path | None = None

    try:
        output_relative = output.relative_to(root)
    except ValueError:
        pass

    # Evita o próprio snapshot entrar no próximo snapshot.
    if output_relative is not None:
        files = [path for path in files if path != output_relative]

    branch = run_git(root, "branch", "--show-current", check=False) or "(detached)"
    commit = run_git(root, "rev-parse", "HEAD", check=False) or "(sem commit)"
    status = run_git(root, "status", "--short", check=False)
    remote = run_git(root, "remote", "get-url", "origin", check=False) or "(sem origin)"
    generated_at = dt.datetime.now().astimezone().isoformat(timespec="seconds")

    lines: list[str] = [
        "PRAETOR — PROJECT CONTEXT SNAPSHOT",
        "=" * 88,
        "",
        f"Repository: {root.name}",
        f"Root: {root}",
        f"Remote origin: {remote}",
        f"Branch: {branch}",
        f"Commit: {commit}",
        f"Generated at: {generated_at}",
        f"Files considered: {len(files)}",
        f"Submodules: {len(submodules)}",
        "",
        "Purpose:",
        "Snapshot textual do repositório Praetor para fornecer contexto completo "
        "de código, documentação e estado Git a uma IA.",
        "A seleção de arquivos respeita as regras de ignore do próprio Git.",
    ]

    append_header(lines, "GIT STATUS")
    lines.append(status if status else "Working tree clean.")

    append_header(lines, "PROJECT TREE")
    lines.append(build_tree(files, submodules))

    if submodules:
        append_header(lines, "SUBMODULES")
        for path, sha in sorted(submodules.items()):
            lines.append(f"{path} -> {sha}")

    append_header(lines, "FILE INVENTORY")
    inventory_count = 0

    for relative in files:
        absolute = root / relative

        if not absolute.exists() or absolute.is_dir():
            continue

        try:
            size = absolute.stat().st_size
        except OSError:
            continue

        binary = is_probably_binary(absolute)
        lines.append(
            f"{relative} | {format_size(size)} | "
            f"{'binary' if binary else language_hint(relative)}"
        )
        inventory_count += 1

    append_header(lines, "FILE CONTENTS")

    included = 0
    skipped_binary = 0
    skipped_large = 0
    skipped_missing = 0

    for relative in files:
        absolute = root / relative

        if not absolute.exists():
            skipped_missing += 1
            continue

        if absolute.is_dir():
            continue

        try:
            size = absolute.stat().st_size
        except OSError:
            skipped_missing += 1
            continue

        lines.extend(
            [
                "",
                "-" * 88,
                f"FILE: {relative}",
                f"SIZE: {format_size(size)}",
                f"TYPE: {language_hint(relative)}",
                "-" * 88,
                "",
            ]
        )

        if size > max_file_bytes:
            lines.append(
                f"[CONTENT OMITTED: file exceeds "
                f"{format_size(max_file_bytes)} limit]"
            )
            skipped_large += 1
            continue

        if is_probably_binary(absolute):
            lines.append("[CONTENT OMITTED: binary file]")
            skipped_binary += 1
            continue

        try:
            content = read_text(absolute)
        except OSError as exc:
            lines.append(f"[CONTENT OMITTED: unable to read: {exc}]")
            skipped_missing += 1
            continue

        lines.append(content.rstrip())
        included += 1

    append_header(lines, "EXPORT SUMMARY")
    lines.extend(
        [
            f"Inventory entries: {inventory_count}",
            f"Text files included: {included}",
            f"Binary files omitted: {skipped_binary}",
            f"Large files omitted: {skipped_large}",
            f"Missing/unreadable entries: {skipped_missing}",
            "",
            "End of Praetor project context.",
        ]
    )

    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text("\n".join(lines) + "\n", encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Exporta árvore, metadados Git e conteúdo textual do projeto Praetor "
            "respeitando as regras de ignore do Git."
        )
    )

    parser.add_argument(
        "--root",
        type=Path,
        default=Path.cwd(),
        help="Diretório dentro do repositório. Default: diretório atual.",
    )

    parser.add_argument(
        "--output",
        type=Path,
        default=None,
        help=f"Arquivo de saída. Default: {DEFAULT_OUTPUT}",
    )

    parser.add_argument(
        "--max-file-bytes",
        type=int,
        default=DEFAULT_MAX_FILE_BYTES,
        help=(
            "Tamanho máximo de um arquivo textual incluído integralmente. "
            f"Default: {DEFAULT_MAX_FILE_BYTES} bytes."
        ),
    )

    return parser.parse_args()


def main() -> int:
    args = parse_args()

    try:
        root = find_git_root(args.root.resolve())

        if args.output is None:
            output = root / DEFAULT_OUTPUT
        elif args.output.is_absolute():
            output = args.output
        else:
            output = root / args.output

        export_context(
            root=root,
            output=output.resolve(),
            max_file_bytes=args.max_file_bytes,
        )

        size = output.stat().st_size

        print("✅ Contexto do Praetor exportado.")
        print(f"Arquivo: {output}")
        print(f"Tamanho: {format_size(size)}")

        return 0

    except Exception as exc:
        print(f"❌ Erro: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
