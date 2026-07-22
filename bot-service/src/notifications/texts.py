from typing import Any

from src.render.cards import CardData
from src.render.theme import GREEN_DEEP, PURPLE, RED

# Telegram shows the caption under the card, so it stays short: the picture carries the detail.
CAPTIONS = {
    "comment": "💬 {actor} прокомментировал ваше фото",
    "vote_request": "🗳 {actor} ждёт голосов в «{room}»",
    "approved": "✅ Тренировка зачтена в «{room}»",
    "rejected": "❌ Тренировку отклонили в «{room}»",
    "buddy_credited": "🤝 Вам зачли совместную тренировку в «{room}»",
    "member_joined": "👋 {actor} присоединился к «{room}»",
    "freeze_scheduled": "❄️ {actor} уходит в заморозку",
    "achievement": "🏅 Новое достижение: {achievement}",
    "reminder": "⏳ Период заканчивается",
    "digest": "📬 Пока бота не было",
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
        actor=payload.get("actor_name", "Кто-то"),
        room=payload.get("room_name", "комнате"),
        achievement=achievement_title(payload.get("key", ""))[0],
    )


def card_for(kind: str, payload: dict[str, Any]) -> CardData:
    room = payload.get("room_name", "")
    actor = payload.get("actor_name", "")

    match kind:
        case "comment":
            return CardData(
                title=f"{actor} оставил комментарий",
                room_name=room,
                actor_name=actor,
                body=payload.get("body", ""),
            )
        case "vote_request":
            return CardData(
                title=f"{actor} отметил тренировку",
                subtitle="Нужен ваш голос",
                room_name=room,
                votes_have=int(payload.get("votes_approve", 0)),
                votes_need=int(payload.get("votes_required", 1)),
                footer="Голосов на зачёт",
            )
        case "approved":
            return CardData(
                title="Тренировка зачтена",
                subtitle="ЗАЧТЕНО",
                room_name=room,
                accent=GREEN_DEEP,
                done=int(payload.get("done", 0)),
                goal=int(payload.get("goal", 0)),
                footer=payload.get("footer", ""),
            )
        case "rejected":
            return CardData(
                title="Тренировку отклонили",
                subtitle="ОТКЛОНЕНО",
                room_name=room,
                accent=RED,
                done=int(payload.get("done", 0)),
                goal=int(payload.get("goal", 0)),
            )
        case "buddy_credited":
            return CardData(
                title="Совместная тренировка зачтена",
                subtitle="ЗАЧТЕНО",
                room_name=room,
                accent=GREEN_DEEP,
                done=int(payload.get("done", 0)),
                goal=int(payload.get("goal", 0)),
            )
        case "member_joined":
            return CardData(
                title=f"{actor} в комнате",
                room_name=room,
                actor_name=actor,
                footer="новый участник",
            )
        case "freeze_scheduled":
            return CardData(
                title=f"{actor} уходит в заморозку",
                room_name=room,
                actor_name=actor,
                accent=PURPLE,
                footer=f"с {payload.get('starts_at', '')} по {payload.get('ends_at', '')}",
            )
        case "achievement":
            title, subtitle = achievement_title(payload.get("key", ""))
            return CardData(
                title=title,
                subtitle=subtitle,
                room_name=room,
                accent=GREEN_DEEP,
                footer="Достижение открыто",
            )
        case "reminder":
            return CardData(
                title="Период заканчивается",
                subtitle=payload.get("subtitle", ""),
                lines=[tuple(line) for line in payload.get("lines", [])],
                footer="Успейте отметить тренировку",
            )
        case "digest":
            return CardData(
                title="Пока бота не было",
                subtitle=payload.get("subtitle", "Собрали в одну сводку"),
                lines=[tuple(line) for line in payload.get("lines", [])],
            )
        case _:
            return CardData(title="BuddyGym", room_name=room)
