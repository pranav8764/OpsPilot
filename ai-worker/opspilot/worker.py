from __future__ import annotations

import asyncio
import json
import logging

from .ingestion import IngestionProcessor, JobNotAvailable


logger = logging.getLogger(__name__)


class BackgroundIngestionWorker:
    def __init__(
        self,
        processor: IngestionProcessor,
        nats_url: str,
        recovery_poll_seconds: float = 5.0,
        retention_cleanup=None,
        retention_cleanup_seconds: float = 3600.0,
    ):
        self.processor = processor
        self.nats_url = nats_url
        self.recovery_poll_seconds = recovery_poll_seconds
        self.retention_cleanup = retention_cleanup
        self.retention_cleanup_seconds = retention_cleanup_seconds
        self._stop = asyncio.Event()

    async def run(self) -> None:
        recovery = asyncio.create_task(self._recovery_loop())
        events = asyncio.create_task(self._nats_loop())
        retention = asyncio.create_task(self._retention_loop()) if self.retention_cleanup else None
        try:
            await self._stop.wait()
        finally:
            recovery.cancel()
            events.cancel()
            tasks = [recovery, events]
            if retention is not None:
                retention.cancel()
                tasks.append(retention)
            await asyncio.gather(*tasks, return_exceptions=True)

    def stop(self) -> None:
        self._stop.set()

    async def _recovery_loop(self) -> None:
        while not self._stop.is_set():
            try:
                await asyncio.to_thread(self.processor.process)
            except JobNotAvailable:
                await asyncio.sleep(self.recovery_poll_seconds)
            except Exception:
                logger.exception("recovered ingestion job failed")
                await asyncio.sleep(self.recovery_poll_seconds)

    async def _nats_loop(self) -> None:
        import nats
        from nats.errors import TimeoutError as NATSTimeoutError
        from nats.js.api import StreamConfig

        while not self._stop.is_set():
            connection = None
            try:
                connection = await nats.connect(self.nats_url, connect_timeout=3, max_reconnect_attempts=10)
                jetstream = connection.jetstream()
                try:
                    await jetstream.add_stream(
                        config=StreamConfig(name="OPSPILOT_INGESTION", subjects=["repo.ingestion.requested"])
                    )
                except Exception as exc:
                    if "stream name already in use" not in str(exc).lower():
                        logger.debug("JetStream add_stream result: %s", exc)
                subscription = await jetstream.pull_subscribe(
                    "repo.ingestion.requested",
                    durable="opspilot-ingestion-workers",
                    stream="OPSPILOT_INGESTION",
                )
                while not self._stop.is_set():
                    try:
                        messages = await subscription.fetch(1, timeout=2)
                    except NATSTimeoutError:
                        continue
                    for message in messages:
                        try:
                            payload = json.loads(message.data)
                            job_id = str(payload["job_id"])
                            await asyncio.to_thread(self.processor.process, job_id)
                            await message.ack()
                        except JobNotAvailable:
                            await message.ack()
                        except Exception:
                            logger.exception("event-driven ingestion job failed")
                            await message.nak(delay=5)
            except asyncio.CancelledError:
                raise
            except Exception:
                logger.exception("NATS ingestion loop disconnected")
                await asyncio.sleep(3)
            finally:
                if connection is not None:
                    await connection.close()

    async def _retention_loop(self) -> None:
        while not self._stop.is_set():
            try:
                deleted = await asyncio.to_thread(self.retention_cleanup)
                if deleted:
                    logger.info("expired stored logs removed", extra={"deleted": deleted})
            except Exception:
                logger.exception("log-retention cleanup failed")
            try:
                await asyncio.wait_for(self._stop.wait(), timeout=self.retention_cleanup_seconds)
            except TimeoutError:
                pass
