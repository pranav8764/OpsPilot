from opspilot.retention import PostgresRetentionCleaner


class FakeConnection:
    def __init__(self):
        self.query = ""

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def execute(self, query):
        self.query = query
        return self

    def fetchone(self):
        return (7,)


def test_retention_cleanup_is_workspace_scoped_and_counts_deletions():
    connection = FakeConnection()
    cleaner = PostgresRetentionCleaner("postgres://test", connect=lambda _url: connection)

    assert cleaner.cleanup() == 7
    assert "log.workspace_id = workspace.id" in connection.query
    assert "workspace.log_retention_days" in connection.query
    assert "DELETE FROM logs" in connection.query
