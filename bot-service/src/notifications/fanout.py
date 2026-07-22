from dataclasses import dataclass
from typing import Any

from src.notifications.events import Event, RoomContext


@dataclass(frozen=True, slots=True)
class Delivery:
    chat_id: int
    kind: str
    payload: dict[str, Any]


def recipients_for(event: Event, room: RoomContext, checkin_owner: int | None) -> list[Delivery]:
    """Who hears about this event, and as what card.

    The actor never gets a card about their own action, and a room the bot cannot resolve
    produces nothing at all.
    """
    others = [uid for uid in room.member_ids if uid != event.actor_id]
    base = {
        "room_id": room.room_id,
        "room_name": room.room_name,
        "actor_id": event.actor_id,
        **event.subject,
    }

    match event.type:
        case "comment.created":
            # the photo owner first, then everyone else who commented is out of scope here
            target = checkin_owner if checkin_owner and checkin_owner != event.actor_id else None
            return [Delivery(target, "comment", base)] if target else []

        case "checkin.created":
            # only members who can still vote need to see it
            if event.subject.get("status") == "approved":
                return []
            return [Delivery(uid, "vote_request", base) for uid in others]

        case "checkin.approved" | "checkin.rejected":
            cards = [Delivery(event.actor_id, event.type.split(".")[1], base)]
            for key in event.subject.get("granted") or []:
                cards.append(Delivery(event.actor_id, "achievement", {**base, "key": key}))
            return cards

        case "buddy.credited":
            return [Delivery(event.actor_id, "buddy_credited", base)]

        case "member.joined":
            return [Delivery(uid, "member_joined", base) for uid in others]

        case "freeze.scheduled":
            return [Delivery(uid, "freeze_scheduled", base) for uid in others]

        case _:
            return []
