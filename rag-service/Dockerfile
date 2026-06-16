FROM python:3.11-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_INDEX_URL=https://pypi.org/simple

WORKDIR /app

COPY rag-service/pyproject.toml ./pyproject.toml
RUN python - <<'PY'
import subprocess
import tomllib

with open("pyproject.toml", "rb") as f:
    deps = tomllib.load(f)["project"]["dependencies"]

subprocess.run(["pip", "install", "--no-cache-dir", *deps], check=True)
PY

COPY rag-service/app ./app

EXPOSE 8000

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
