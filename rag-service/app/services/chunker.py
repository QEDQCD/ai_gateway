def chunk_document(content: str, max_chars: int = 500, overlap: int = 80) -> list[str]:
    stripped = content.strip()
    if not stripped:
        return []

    paragraphs = [segment.strip() for segment in stripped.split("\n") if segment.strip()]
    chunks: list[str] = []
    current = ""

    for paragraph in paragraphs:
        candidate = paragraph if not current else f"{current}\n{paragraph}"
        if len(candidate) <= max_chars:
            current = candidate
            continue

        if current:
            chunks.append(current)
            prefix = current[-overlap:].strip()
            current = f"{prefix}\n{paragraph}".strip() if prefix else paragraph
        else:
            chunks.extend(split_long_text(paragraph, max_chars=max_chars, overlap=overlap))
            current = ""

    if current:
        chunks.append(current)

    return chunks or [stripped]


def split_long_text(content: str, max_chars: int, overlap: int) -> list[str]:
    parts: list[str] = []
    start = 0
    while start < len(content):
        end = min(len(content), start + max_chars)
        parts.append(content[start:end].strip())
        if end >= len(content):
            break
        start = max(0, end - overlap)
    return [part for part in parts if part]
