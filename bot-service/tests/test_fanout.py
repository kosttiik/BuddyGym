from datetime import UTC, datetime

from src.notifications.events import Event, RoomContext
from src.notifications.fanout import recipients_for

ROOM = RoomContext(
    room_id=7,
    room_name="Железные братья",
    avatar_key="",
    goal_per_period=3,
    period_days=7,
    votes_required=2,
    member_ids=(1, 2, 3),
)


def event(type_: str, actor: int = 1, **subject) -> Event:
    return Event(
        id=10,
        type=type_,
        room_id=ROOM.room_id,
        actor_id=actor,
        subject=subject,
        created_at=datetime(2026, 7, 22, tzinfo=UTC),
    )


def test_a_comment_reaches_the_photo_owner_only():
    deliveries = recipients_for(event("comment.created", actor=2, checkin_id="c1"), ROOM, 3)

    assert [(d.chat_id, d.kind) for d in deliveries] == [(3, "comment")]


def test_a_comment_on_your_own_photo_notifies_nobody():
    assert recipients_for(event("comment.created", actor=3, checkin_id="c1"), ROOM, 3) == []


def test_a_pending_checkin_asks_every_other_member_to_vote():
    deliveries = recipients_for(event("checkin.created", actor=1, status="pending"), ROOM, None)

    assert sorted(d.chat_id for d in deliveries) == [2, 3]
    assert {d.kind for d in deliveries} == {"vote_request"}


def test_a_geo_checkin_asks_for_no_votes():
    assert recipients_for(event("checkin.created", actor=1, status="approved"), ROOM, None) == []


def test_an_approval_notifies_the_author_and_carries_achievements():
    deliveries = recipients_for(
        event("checkin.approved", actor=2, checkin_id="c1", granted=["streak_7"]), ROOM, None
    )

    assert [(d.chat_id, d.kind) for d in deliveries] == [(2, "approved"), (2, "achievement")]
    assert deliveries[1].payload["key"] == "streak_7"


def test_unknown_events_are_ignored():
    assert recipients_for(event("room.renamed"), ROOM, None) == []


def test_a_reply_reaches_the_author_and_the_photo_owner():
    # the comment author gets a reply card, the photo owner still learns a comment landed
    deliveries = recipients_for(
        event("comment.created", actor=2, checkin_id="c1", reply_to_author_id=1), ROOM, 3
    )

    assert [(d.chat_id, d.kind) for d in deliveries] == [(1, "reply"), (3, "comment")]


def test_replying_to_your_own_comment_still_tells_the_photo_owner():
    deliveries = recipients_for(
        event("comment.created", actor=2, checkin_id="c1", reply_to_author_id=2), ROOM, 3
    )

    assert [(d.chat_id, d.kind) for d in deliveries] == [(3, "comment")]


def test_a_reply_to_the_photo_owner_is_not_doubled():
    # owner is the comment author too: one reply card, no duplicate comment card
    deliveries = recipients_for(
        event("comment.created", actor=2, checkin_id="c1", reply_to_author_id=3), ROOM, 3
    )

    assert [(d.chat_id, d.kind) for d in deliveries] == [(3, "reply")]
