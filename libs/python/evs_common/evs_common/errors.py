"""Typed domain errors mirroring gRPC status codes."""
from enum import IntEnum


class Code(IntEnum):
    OK = 0
    NOT_FOUND = 5
    ALREADY_EXISTS = 6
    INVALID_ARGUMENT = 3
    PERMISSION_DENIED = 7
    UNAUTHENTICATED = 16
    INTERNAL = 13
    UNAVAILABLE = 14
    RESOURCE_EXHAUSTED = 8


class DomainError(Exception):
    def __init__(self, code: Code, message: str, cause: Exception | None = None):
        super().__init__(message)
        self.code = code
        self.cause = cause

    def __str__(self) -> str:
        if self.cause:
            return f"{super().__str__()}: {self.cause}"
        return super().__str__()


def not_found(message: str) -> DomainError:
    return DomainError(Code.NOT_FOUND, message)


def invalid_argument(message: str) -> DomainError:
    return DomainError(Code.INVALID_ARGUMENT, message)


def internal(message: str, cause: Exception | None = None) -> DomainError:
    return DomainError(Code.INTERNAL, message, cause)
