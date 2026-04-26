FROM compose-rag-service:latest

ENV PIP_INDEX_URL=https://pypi.org/simple

WORKDIR /app

RUN python -m pip install --no-cache-dir "pg8000>=1.31,<2.0"

COPY rag-service/app ./app

EXPOSE 8000

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]
