FROM python:3.11-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_INDEX_URL=https://pypi.org/simple

WORKDIR /app

RUN pip install --no-cache-dir "fastapi>=0.110,<1.0" "uvicorn>=0.29,<1.0"

COPY model-service/app ./app

EXPOSE 8001

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8001"]
