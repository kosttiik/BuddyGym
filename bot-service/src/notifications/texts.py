from typing import Any

from src.render.cards import CardData
from src.render.theme import AMBER, GREEN_DEEP, INK_SOFT, PURPLE, RED

# Russian verbs carry gender, and a name alone does not tell us which one to use, so every
# string here is built from nouns instead of past-tense verbs.
CAPTIONS = {
    "comment": "💬 Новый комментарий к вашему фото",
    "vote_request": "🗳 Нужен ваш голос в «{room}»",
    "vote_last_call": "⏰ Голосование скоро закроется",
    "approved": "✅ Тренировка зачтена в «{room}»",
    "rejected": "❌ Тренировка отклонена в «{room}»",
    "buddy_credited": "🤝 Совместная тренировка зачтена в «{room}»",
    "member_joined": "👋 Новый участник в «{room}»",
    "member_left": "🚪 Участник покинул комнату «{room}»",
    "freeze_scheduled": "❄️ Заморозка в «{room}»",
    "freeze_canceled": "🔥 Заморозка снята в «{room}»",
    "achievement": "🏅 Новое достижение: {achievement}",
    "reminder": "⏳ Период заканчивается",
    "streak_at_risk": "🔥 Серия под угрозой",
    "period_summary": "📊 Итоги периода",
    "digest": "📬 Сводка BuddyGym",
    "welcome": "👋 BuddyGym на связи",
}

ACHIEVEMENTS = {
    "first_checkin": ("Первый чек-ин", "Первая отмеченная тренировка"),
    "workouts_10": ("10 тренировок", "Десять зачтённых тренировок"),
    "workouts_50": ("50 тренировок", "Полсотни за плечами"),
    "workouts_250": ("250 тренировок", "Уровень зверя"),
    "streak_7": ("Неделя подряд", "7 дней без пропусков"),
    "streak_14": ("Две недели подряд", "14 дней без пропусков"),
    "streak_30": ("Месяц подряд", "30 дней без пропусков"),
    "rooms_3": ("Три комнаты", "Тренируетесь сразу с несколькими компаниями"),
    "buddies_5": ("Пять напарников", "Тренировки вместе засчитаны"),
    "comments_10": ("Душа компании", "Десять комментариев"),
    "early_bird": ("Ранняя пташка", "Тренировка до восьми утра"),
    "night_owl": ("Сова", "Тренировка после десяти вечера"),
}


def achievement_title(key: str) -> tuple[str, str]:
    return ACHIEVEMENTS.get(key, (key, "Новое достижение"))


def caption(kind: str, payload: dict[str, Any]) -> str:
    template = CAPTIONS.get(kind, "BuddyGym")
    return template.format(
        actor=payload.get("actor_name", "Участник"),
        room=payload.get("room_name", "комнате"),
        achievement=achievement_title(payload.get("key", ""))[0],
    )


def _lines(payload: dict[str, Any]) -> list[tuple[str, int, int]]:
    return [tuple(line) for line in payload.get("lines", [])]


def card_for(kind: str, payload: dict[str, Any]) -> CardData:
    room = payload.get("room_name", "")
    actor = payload.get("actor_name", "")

    match kind:
        case "comment":
            return CardData(
                title="Новый комментарий",
                subtitle="под вашей тренировкой",
                room_name=room,
                actor_name=actor,
                body=payload.get("body", ""),
            )
        case "vote_request":
            return CardData(
                title="Нужен ваш голос",
                subtitle="Тренировка ждёт подтверждения",
                room_name=room,
                actor_name=actor,
                votes_have=int(payload.get("votes_approve", 0)),
                votes_need=int(payload.get("votes_required", 1)),
                footer="Голосов на зачёт",
            )
        case "vote_last_call":
            return CardData(
                title="Голосование закрывается",
                subtitle=payload.get("subtitle", "Осталось меньше трёх часов"),
                room_name=room,
                actor_name=actor,
                accent=AMBER,
                votes_have=int(payload.get("votes_approve", 0)),
                votes_need=int(payload.get("votes_required", 1)),
                footer="Голосов на зачёт",
            )
        case "approved":
            return CardData(
                title="Тренировка зачтена",
                badge="ЗАЧТЕНО",
                room_name=room,
                accent=GREEN_DEEP,
                done=int(payload.get("done", 0)),
                goal=int(payload.get("goal", 0)),
                footer=payload.get("footer", ""),
            )
        case "rejected":
            return CardData(
                title="Тренировка отклонена",
                badge="ОТКЛОНЕНО",
                room_name=room,
                accent=RED,
                done=int(payload.get("done", 0)),
                goal=int(payload.get("goal", 0)),
            )
        case "buddy_credited":
            return CardData(
                title="Совместная тренировка зачтена",
                badge="ЗАЧТЕНО",
                room_name=room,
                accent=GREEN_DEEP,
                done=int(payload.get("done", 0)),
                goal=int(payload.get("goal", 0)),
            )
        case "member_joined":
            return CardData(
                title="Новый участник",
                room_name=room,
                actor_name=actor,
                footer="теперь тренируется с вами",
            )
        case "member_left":
            return CardData(
                title="Участник вышел",
                room_name=room,
                actor_name=actor,
                accent=INK_SOFT,
                footer="покинул комнату",
            )
        case "freeze_scheduled":
            return CardData(
                title="Заморозка",
                room_name=room,
                actor_name=actor,
                accent=PURPLE,
                footer=f"с {payload.get('starts_at', '')} по {payload.get('ends_at', '')}",
            )
        case "freeze_canceled":
            return CardData(
                title="Заморозка снята",
                room_name=room,
                actor_name=actor,
                accent=GREEN_DEEP,
                footer="снова в деле",
            )
        case "achievement":
            title, subtitle = achievement_title(payload.get("key", ""))
            return CardData(title=title, subtitle=subtitle, room_name=room, accent=GREEN_DEEP)
        case "reminder":
            return CardData(
                title="Период заканчивается",
                subtitle=payload.get("subtitle", ""),
                lines=_lines(payload),
                footer="Ещё есть время отметить тренировку",
            )
        case "streak_at_risk":
            return CardData(
                title="Серия под угрозой",
                subtitle=payload.get("subtitle", ""),
                accent=AMBER,
                lines=_lines(payload),
                footer="Одна тренировка спасёт серию",
            )
        case "period_summary":
            return CardData(
                title="Период закрыт",
                subtitle=payload.get("subtitle", ""),
                lines=_lines(payload),
                footer=payload.get("footer", ""),
            )
        case "digest":
            return CardData(
                title="Что произошло",
                subtitle=payload.get("subtitle", "Собрали в одну сводку"),
                lines=_lines(payload),
            )
        case "welcome":
            return CardData(
                title="BuddyGym на связи",
                subtitle="Буду присылать только важное",
                lines=[
                    ("Комментарии к вашим фото", 0, 0),
                    ("Голосования и зачёты", 0, 0),
                    ("Достижения", 0, 0),
                    ("Напоминание перед концом периода", 0, 0),
                ],
            )
        case _:
            return CardData(title="BuddyGym", room_name=room)
