FROM compose-rag-service:latest

WORKDIR /app

COPY model-service/app ./app

EXPOSE 8001

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8001"]
