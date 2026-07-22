import logging

import aioboto3
import grpc
from buddygym.v1 import checkin_pb2, checkin_pb2_grpc

from src.core.config import Settings

MAX_PHOTO_BYTES = 12 * 1024 * 1024


class PhotoSource:
    """Reads the bytes a card needs.

    Avatars and comment pictures come straight from object storage; checkin proofs live in
    checkin-service and are streamed over its existing gRPC method.
    """

    def __init__(self, settings: Settings, logger: logging.Logger | None = None) -> None:
        self._settings = settings
        self._log = logger or logging.getLogger(__name__)
        self._session = aioboto3.Session()
        self._channel: grpc.aio.Channel | None = None

    def _s3(self):
        return self._session.client(
            "s3",
            endpoint_url=self._settings.s3_endpoint,
            aws_access_key_id=self._settings.s3_access_key,
            aws_secret_access_key=self._settings.s3_secret_key,
        )

    async def _object(self, bucket: str, key: str) -> bytes | None:
        if not key or not self._settings.s3_endpoint:
            return None
        try:
            async with self._s3() as client:
                response = await client.get_object(Bucket=bucket, Key=key)
                async with response["Body"] as stream:
                    return await stream.read()
        except Exception as error:
            self._log.warning("read %s/%s failed: %s", bucket, key, error)
            return None

    async def avatar(self, key: str) -> bytes | None:
        return await self._object(self._settings.s3_avatar_bucket, key)

    async def comment_photo(self, key: str) -> bytes | None:
        return await self._object(self._settings.s3_comment_bucket, key)

    async def checkin_photo(self, checkin_id: str) -> bytes | None:
        if self._channel is None:
            self._channel = grpc.aio.insecure_channel(
                self._settings.checkin_grpc_addr,
                options=[("grpc.max_receive_message_length", MAX_PHOTO_BYTES)],
            )
        stub = checkin_pb2_grpc.CheckinServiceStub(self._channel)
        chunks: list[bytes] = []
        try:
            async for chunk in stub.GetCheckinPhoto(
                checkin_pb2.GetCheckinPhotoRequest(checkin_id=checkin_id)
            ):
                chunks.append(chunk.data)
        except grpc.aio.AioRpcError as error:
            # a purged or geo checkin simply has no picture, which is not an error here
            if error.code() not in {grpc.StatusCode.NOT_FOUND, grpc.StatusCode.FAILED_PRECONDITION}:
                self._log.warning("checkin photo %s failed: %s", checkin_id, error.code())
            return None
        return b"".join(chunks) or None

    async def close(self) -> None:
        if self._channel is not None:
            await self._channel.close()
            self._channel = None
