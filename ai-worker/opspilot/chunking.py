from __future__ import annotations

from .domain import Chunk


def chunk_text(text: str, max_chars: int = 4000, overlap_chars: int = 400) -> list[Chunk]:
    if max_chars < 200:
        raise ValueError("max_chars must be at least 200")
    if overlap_chars < 0 or overlap_chars >= max_chars:
        raise ValueError("overlap_chars must be non-negative and smaller than max_chars")

    source_lines = text.splitlines()
    if not source_lines:
        return []

    segments: list[tuple[str, int]] = []
    for line_number, line in enumerate(source_lines, start=1):
        if len(line) <= max_chars:
            segments.append((line, line_number))
            continue
        for offset in range(0, len(line), max_chars):
            segments.append((line[offset : offset + max_chars], line_number))

    chunks: list[Chunk] = []
    start = 0
    while start < len(segments):
        end = start
        size = 0
        pieces: list[str] = []
        while end < len(segments):
            line = segments[end][0]
            added = len(line) + (1 if pieces else 0)
            if pieces and size + added > max_chars:
                break
            pieces.append(line)
            size += added
            end += 1

        if not pieces:
            break

        chunks.append(
            Chunk(
                text="\n".join(pieces),
                index=len(chunks),
                start_line=segments[start][1],
                end_line=segments[end - 1][1],
            )
        )
        if end >= len(segments):
            break

        overlap_size = 0
        next_start = end
        while next_start > start and overlap_size < overlap_chars:
            next_start -= 1
            overlap_size += len(segments[next_start][0]) + 1
        start = next_start if next_start > start else end

    return chunks
