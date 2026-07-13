from opspilot.security import path_policy, scan_and_redact


def test_path_policy_blocks_secrets_generated_and_dependencies():
    assert path_policy(".env", 10, 1000)[0] is False
    assert path_policy("config/.env.production", 10, 1000)[0] is False
    assert path_policy("node_modules/pkg/index.js", 10, 1000)[0] is False
    assert path_policy("secrets/server.pem", 10, 1000)[0] is False
    assert path_policy("public/app.min.js", 10, 1000)[0] is False
    assert path_policy("src/huge.py", 1001, 1000)[0] is False


def test_path_policy_allows_supported_source_and_config_files():
    assert path_policy("README.md", 100, 1000)[0] is True
    assert path_policy("services/api/main.go", 100, 1000)[0] is True
    assert path_policy("Dockerfile", 100, 1000)[0] is True


def test_scan_redacts_tokens_keys_and_secret_assignments():
    github_token = "ghp_abcdefghijklmnopqrstuvwxyz1234567890ABCD"
    content = f"API_KEY=super-secret-value-123\ntoken={github_token}\nAuthorization: Bearer abcdefghijklmnopqrstuvwxyz.123456\n"
    result = scan_and_redact(content)

    assert "super-secret-value-123" not in result.content
    assert github_token not in result.content
    assert "abcdefghijklmnopqrstuvwxyz.123456" not in result.content
    assert result.redaction_count == 3


def test_scan_rejects_binary_content():
    try:
        scan_and_redact("hello\x00world")
    except ValueError as exc:
        assert "binary" in str(exc)
    else:
        raise AssertionError("binary content should be rejected")
