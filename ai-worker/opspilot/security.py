from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import PurePosixPath


ALLOWED_EXTENSIONS = {
    ".c", ".cc", ".conf", ".cpp", ".cs", ".css", ".dockerfile", ".go", ".gradle",
    ".graphql", ".h", ".hpp", ".html", ".ini", ".java", ".js", ".json", ".jsx",
    ".kt", ".kts", ".md", ".mdx", ".php", ".properties", ".proto", ".py", ".rb",
    ".rs", ".scala", ".sh", ".sql", ".toml", ".ts", ".tsx", ".txt", ".xml",
    ".yaml", ".yml",
}

ALLOWED_FILENAMES = {
    "dockerfile", "makefile", "procfile", "gemfile", "rakefile", "go.mod", "go.sum",
    "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "requirements.txt",
}

BLOCKED_FILENAMES = {
    ".env", ".env.local", ".env.production", ".env.development", ".npmrc", ".pypirc",
    ".netrc", "credentials", "credentials.json", "id_rsa", "id_ed25519",
}

BLOCKED_EXTENSIONS = {".pem", ".key", ".p12", ".pfx", ".jks", ".keystore", ".der", ".crt"}

BLOCKED_DIRECTORIES = {
    ".git", ".next", ".nuxt", ".terraform", ".venv", "bin", "build", "coverage", "dist",
    "node_modules", "obj", "target", "vendor", "venv", "__pycache__",
}

GENERATED_MARKERS = {".min.js", ".min.css", ".map", ".lock"}

SECRET_PATTERNS = {
    "private_key": re.compile(r"-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----"),
    "github_token": re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,})\b"),
    "aws_access_key": re.compile(r"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b"),
    "slack_token": re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{20,}\b"),
    "bearer_token": re.compile(r"(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{20,}"),
    "secret_assignment": re.compile(
        r"(?im)^(\s*(?:api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret|password|private[_-]?key)\s*[:=]\s*)"
        r"([^\s#'\"]{12,}|['\"][^'\"\n]{12,}['\"])(\s*(?:#.*)?)$"
    ),
    "inline_secret_assignment": re.compile(
        r"(?i)(\b(?:api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret|password|private[_-]?key)\s*[:=]\s*)"
        r"([^\s,;]{12,})"
    ),
}


@dataclass(frozen=True)
class ScanResult:
    content: str
    redactions: dict[str, int]

    @property
    def redaction_count(self) -> int:
        return sum(self.redactions.values())


def path_policy(path: str, size: int, max_file_bytes: int) -> tuple[bool, str]:
    normalized = PurePosixPath(path)
    parts = [part.lower() for part in normalized.parts]
    name = normalized.name.lower()
    suffix = normalized.suffix.lower()

    if not path or path.startswith("/") or ".." in normalized.parts:
        return False, "invalid_path"
    if any(part in BLOCKED_DIRECTORIES for part in parts[:-1]):
        return False, "blocked_directory"
    if name in BLOCKED_FILENAMES or name.startswith(".env."):
        return False, "secret_file"
    if suffix in BLOCKED_EXTENSIONS:
        return False, "secret_file"
    if size < 0 or size > max_file_bytes:
        return False, "file_too_large"
    if any(name.endswith(marker) for marker in GENERATED_MARKERS):
        return False, "generated_file"
    if name not in ALLOWED_FILENAMES and suffix not in ALLOWED_EXTENSIONS:
        return False, "unsupported_type"
    return True, "allowed"


def scan_and_redact(content: str) -> ScanResult:
    if "\x00" in content:
        raise ValueError("binary content")

    redacted = content
    counts: dict[str, int] = {}
    for detector, pattern in SECRET_PATTERNS.items():
        if detector == "secret_assignment":
            redacted, count = pattern.subn(r"\1[REDACTED]\3", redacted)
        elif detector == "inline_secret_assignment":
            redacted, count = pattern.subn(r"\1[REDACTED]", redacted)
        else:
            redacted, count = pattern.subn("[REDACTED]", redacted)
        if count:
            counts[detector] = count
    return ScanResult(content=redacted, redactions=counts)


def file_metadata(path: str) -> tuple[str, str]:
    name = PurePosixPath(path).name.lower()
    suffix = PurePosixPath(path).suffix.lower().lstrip(".")
    file_type = "config" if name in {"dockerfile", "makefile", "procfile"} or suffix in {"yaml", "yml", "json", "toml", "ini", "conf", "properties"} else "code"
    if suffix in {"md", "mdx", "txt"}:
        file_type = "documentation"
    language = {
        "yml": "yaml", "py": "python", "js": "javascript", "jsx": "javascript",
        "ts": "typescript", "tsx": "typescript", "rb": "ruby", "rs": "rust",
        "sh": "shell", "md": "markdown", "mdx": "markdown",
    }.get(suffix, suffix or name)
    return file_type, language
