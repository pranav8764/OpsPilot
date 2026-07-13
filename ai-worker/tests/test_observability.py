from fastapi.testclient import TestClient

from main import app


def test_request_id_is_preserved_or_replaced_and_metrics_are_low_cardinality():
    client = TestClient(app)
    response = client.get("/health", headers={"X-Request-ID": "safe-id_1"})
    assert response.status_code == 200
    assert response.headers["X-Request-ID"] == "safe-id_1"

    response = client.get("/health", headers={"X-Request-ID": "unsafe value"})
    assert response.status_code == 200
    assert response.headers["X-Request-ID"] != "unsafe value"

    metrics = client.get("/metrics").text
    assert 'opspilot_ai_http_requests_total{method="GET",status="200"}' in metrics
    assert "safe-id_1" not in metrics
