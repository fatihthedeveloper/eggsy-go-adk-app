"""
Slack receiver Lambda.

The lightweight, latency-critical front door for Slack events. Its only job is to
answer Slack within ~2s, so it does the bare minimum:

  1. Verify the Slack request signature (HMAC-SHA256) + replay window.
  2. Answer Slack's url_verification handshake.
  3. Forward the RAW request body verbatim to the worker Lambda via an async
     (fire-and-forget) invoke.
  4. Return 200 immediately.

It contains NO business logic: no email construction, no command shaping, no Slack
history fetch. All of that lives in the Go worker, which parses the forwarded Slack
event itself. The only contract between the two functions is Slack's own event JSON.

Runtime:  python3.13 (boto3 is provided by the runtime — no dependencies to package)
Handler:  handler.handler
Trigger:  Lambda Function URL (payload format 2.0)

Required environment variables:
  SLACK_SIGNING_SECRET  - Slack app signing secret, used to verify request signatures.
  WORKER_FUNCTION_NAME  - name or ARN of the Go worker Lambda to invoke.

Required IAM permission on this function's execution role:
  lambda:InvokeFunction on the worker function's ARN.
"""

import base64
import hashlib
import hmac
import json
import logging
import os
import time

import boto3

logger = logging.getLogger()
logger.setLevel(logging.INFO)

# Reused across warm invocations.
_lambda = boto3.client("lambda")

SIGNING_SECRET = os.environ["SLACK_SIGNING_SECRET"].encode()
WORKER_FUNCTION_NAME = os.environ["WORKER_FUNCTION_NAME"]

# Reject requests whose timestamp is older than this (replay-attack guard). Slack
# recommends 5 minutes.
MAX_TIMESTAMP_SKEW_SECONDS = 60 * 5


def _response(status_code: int, body: str = ""):
    return {"statusCode": status_code, "body": body}


def _verify_signature(headers: dict, raw_body: str) -> bool:
    """Validate Slack's v0 request signature against the raw body + timestamp."""
    timestamp = headers.get("x-slack-request-timestamp", "")
    signature = headers.get("x-slack-signature", "")
    if not timestamp or not signature:
        logger.warning("missing slack signature headers")
        return False

    try:
        skew = abs(time.time() - int(timestamp))
    except ValueError:
        logger.warning("invalid slack timestamp header: %r", timestamp)
        return False
    if skew > MAX_TIMESTAMP_SKEW_SECONDS:
        logger.warning("slack timestamp outside allowed window (skew=%ss)", skew)
        return False

    basestring = f"v0:{timestamp}:{raw_body}".encode()
    expected = "v0=" + hmac.new(SIGNING_SECRET, basestring, hashlib.sha256).hexdigest()

    # Constant-time comparison to avoid timing attacks.
    return hmac.compare_digest(expected, signature)


def handler(event, _context):
    # --- extract the raw request body exactly as Slack sent it ---
    raw_body = event.get("body") or ""
    if event.get("isBase64Encoded"):
        raw_body = base64.b64decode(raw_body).decode("utf-8")

    try:
        parsed = json.loads(raw_body)
    except (ValueError, TypeError):
        logger.warning("request body was not valid JSON")
        return _response(400, "Bad Request")

    # --- 1. Slack URL verification handshake (no signature is sent for this) ---
    if parsed.get("type") == "url_verification":
        return _response(200, parsed.get("challenge", ""))

    # Function URLs (payload format 2.0) lower-case header names, but normalize
    # defensively in case this is fronted by API Gateway instead.
    headers = {k.lower(): v for k, v in (event.get("headers") or {}).items()}

    # --- 2. Verify the request really came from Slack ---
    if not _verify_signature(headers, raw_body):
        logger.warning("untrusted source: signature verification failed")
        return _response(401, "Untrusted Source!")

    # --- Optional: skip Slack's automatic retries to avoid double-processing ---
    # The worker has side effects (it records transactions), and a duplicate run is
    # worse than a rarely-missed retry given we ack fast. Slack sets this header on
    # retried deliveries. Remove this block if you'd rather process every retry.
    if "x-slack-retry-num" in headers:
        logger.info(
            "ignoring slack retry (reason=%s, num=%s)",
            headers.get("x-slack-retry-reason"),
            headers.get("x-slack-retry-num"),
        )
        return _response(200)

    # --- 3. Forward the raw event verbatim to the worker (async / fire-and-forget) ---
    try:
        _lambda.invoke(
            FunctionName=WORKER_FUNCTION_NAME,
            InvocationType="Event",  # async: returns immediately, does not wait
            Payload=raw_body.encode("utf-8"),
        )
    except Exception:
        # Log but still 200: telling Slack we failed would trigger its retry storm,
        # and the failure is on our side (e.g. throttling), not Slack's.
        logger.exception("failed to invoke worker lambda")

    # --- 4. Acknowledge Slack immediately. The worker sets the first emoji reaction. ---
    return _response(200)
