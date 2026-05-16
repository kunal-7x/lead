from evs_common.errors import Code, DomainError, not_found, invalid_argument, internal


def test_not_found():
    err = not_found("lead not found")
    assert err.code == Code.NOT_FOUND
    assert "lead not found" in str(err)


def test_invalid_argument():
    err = invalid_argument("bad input")
    assert err.code == Code.INVALID_ARGUMENT


def test_internal_with_cause():
    cause = ValueError("oops")
    err = internal("db error", cause)
    assert err.code == Code.INTERNAL
    assert err.cause is cause
    assert "oops" in str(err)


def test_domain_error_is_exception():
    err = DomainError(Code.UNAUTHENTICATED, "no auth")
    assert isinstance(err, Exception)
