import pytest

from eval_harness import EvalHarness, ReviewService, seed_store


@pytest.fixture()
def harness() -> EvalHarness:
    return EvalHarness(seed_store())


@pytest.fixture()
def review_service(harness: EvalHarness) -> ReviewService:
    return ReviewService(harness.store)
