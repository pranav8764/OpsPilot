FROM python:3.12-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

RUN groupadd --system --gid 10001 opspilot \
    && useradd --system --uid 10001 --gid opspilot --home-dir /app opspilot \
    && chown -R opspilot:opspilot /app

USER 10001:10001

EXPOSE 8090

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8090", "--no-access-log"]
