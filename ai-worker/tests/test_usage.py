from opspilot.usage import PLAN_LIMITS, UsageLimitExceeded, storage_limit_for_plan


def test_plan_limits_scale_and_unknown_plan_fails_closed():
    assert PLAN_LIMITS["free"]["fix_proposal"] < PLAN_LIMITS["pro"]["fix_proposal"] < PLAN_LIMITS["team"]["fix_proposal"]
    assert storage_limit_for_plan("unknown") == PLAN_LIMITS["free"]["indexed_storage_bytes"]


def test_usage_limit_error_is_explicit():
    error = UsageLimitExceeded("ai_query", 200, 200)
    assert "200/200" in str(error)
    assert error.event_type == "ai_query"
