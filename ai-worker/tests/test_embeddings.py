from opspilot.embeddings import LocalHashEmbeddings


def dot(left, right):
    assert len(left) == len(right)
    return sum(a * b for a, b in zip(left, right))


def test_local_embeddings_are_deterministic_and_lexically_useful():
    provider = LocalHashEmbeddings(128)
    query = provider.embed_query("database connection pool timeout")
    related = provider.embed_query("database pool configuration")
    unrelated = provider.embed_query("frontend typography animation")

    assert query == provider.embed_query("database connection pool timeout")
    assert dot(query, related) > dot(query, unrelated)
    assert abs(dot(query, query) - 1.0) < 1e-9
