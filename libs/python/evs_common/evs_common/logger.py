"""Structured logger with OpenTelemetry trace correlation."""
import logging
import structlog
from opentelemetry import trace


def configure(service: str, level: str = "INFO") -> None:
    """Call once at service startup to configure structlog + stdlib logging."""
    logging.basicConfig(level=getattr(logging, level.upper(), logging.INFO))
    structlog.configure(
        processors=[
            structlog.contextvars.merge_contextvars,
            _add_trace_context,
            structlog.processors.add_log_level,
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.JSONRenderer(),
        ],
        wrapper_class=structlog.make_filtering_bound_logger(
            getattr(logging, level.upper(), logging.INFO)
        ),
        context_class=dict,
        logger_factory=structlog.PrintLoggerFactory(),
    )
    structlog.contextvars.bind_contextvars(service=service)


def get(name: str = __name__) -> structlog.BoundLogger:
    """Return a bound structlog logger."""
    return structlog.get_logger(name)


def _add_trace_context(
    logger: object, method_name: str, event_dict: dict
) -> dict:
    span = trace.get_current_span()
    ctx = span.get_span_context()
    if ctx.is_valid:
        event_dict["trace_id"] = format(ctx.trace_id, "032x")
        event_dict["span_id"] = format(ctx.span_id, "016x")
    return event_dict
