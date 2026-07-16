from opspilot.chunking import chunk_text


def test_chunking_preserves_content_and_source_lines_with_overlap():
    text = "\n".join(f"line-{number}-" + ("x" * 30) for number in range(1, 20))
    chunks = chunk_text(text, max_chars=200, overlap_chars=50)

    assert len(chunks) > 1
    assert chunks[0].start_line == 1
    assert chunks[-1].end_line == 19
    assert chunks[1].start_line <= chunks[0].end_line
    assert all(len(chunk.text) <= 200 for chunk in chunks)


def test_chunking_does_not_drop_very_long_lines():
    text = "z" * 550
    chunks = chunk_text(text, max_chars=200, overlap_chars=0)

    assert "".join(chunk.text for chunk in chunks) == text
    assert all(chunk.start_line == 1 and chunk.end_line == 1 for chunk in chunks)


def test_chunking_validates_limits():
    for args in [(100, 0), (200, 200), (200, -1)]:
        try:
            chunk_text("content", max_chars=args[0], overlap_chars=args[1])
        except ValueError:
            pass
        else:
            raise AssertionError(f"invalid chunk limits should fail: {args}")
