FROM python:3.12-slim

WORKDIR /app

RUN pip install --no-cache-dir poetry

COPY services/parkings-storage/pyproject.toml .
COPY services/parkings-storage/poetry.lock .

COPY services/parkings-storage/ .
RUN poetry install --no-root --no-dev

ENV PORT=8000

ENTRYPOINT poetry run uvicorn --host 0.0.0.0 --port "$PORT" parkings_storage:app
