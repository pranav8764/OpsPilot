from opspilot.deployment import detect_stack, discover_env_names, generate_deployment_files, readiness_report
from opspilot.domain import RepositoryFile


def repo_files(include_docker=False):
    files = [
        RepositoryFile("package.json", "1", 100, '{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"16.0.0"}}'),
        RepositoryFile("package-lock.json", "2", 2, "{}"),
        RepositoryFile("src/api.ts", "3", 80, "const key = process.env.DATABASE_URL; app.get('/health', ok); const port = process.env.PORT"),
    ]
    if include_docker:
        files.append(RepositoryFile("Dockerfile", "4", 80, "FROM node:22\nUSER node\nHEALTHCHECK CMD true\n"))
    return files


def test_readiness_scan_is_grounded_and_scores_blockers():
    report = readiness_report("commit-1", repo_files())
    assert report["stack"]["framework"] == "nextjs"
    assert report["ready"] is False
    assert any(item["code"] == "missing_dockerfile" for item in report["findings"])
    assert report["provider_recommendations"][0]["provider"] == "vercel"
    assert "DATABASE_URL" in report["required_env_names"]


def test_deterministic_generator_contains_no_secret_values_and_valid_compose():
    generated = generate_deployment_files("commit-1", repo_files())
    assert "FROM node:22-bookworm-slim" in generated["dockerfile"]
    assert "COPY package*.json ./" in generated["dockerfile"]
    assert "USER node" in generated["dockerfile"]
    assert "DATABASE_URL=" in generated["env_example"]
    assert "services:" in generated["compose_yaml"]
    assert "secret" not in generated["env_example"].lower()


def test_stack_and_env_detection_do_not_read_dotenv_values():
    files = {"requirements.txt": "fastapi", "main.py": "os.getenv('API_TOKEN')", ".env": "LEAKED=value"}
    assert detect_stack(files)["framework"] == "fastapi"
    assert discover_env_names(files) == ["API_TOKEN"]


def test_generate_go_dockerfile_without_go_sum():
    files = [RepositoryFile("go.mod", "1", 43, "module example.com/service\n\ngo 1.23\n")]

    result = generate_deployment_files("c" * 40, files)

    assert "COPY go.mod ./" in result["dockerfile"]
    assert "go.sum" not in result["dockerfile"]
