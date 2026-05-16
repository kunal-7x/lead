from evs_common import logger


def test_get_logger():
    l = logger.get("test")
    assert l is not None


def test_configure_does_not_raise():
    logger.configure("test-service")
