FROM python:3.12-alpine

WORKDIR /app

RUN apk add --no-cache docker-cli

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8092

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8092", "--no-access-log"]
