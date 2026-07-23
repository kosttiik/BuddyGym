from datetime import date
from typing import Any

from src.render.cards import CardData
from src.render.theme import AMBER, GREEN_DEEP, INK_SOFT, PURPLE, RED

MONTHS = {
    # genitive: the date always reads as "25 июля", never "25 июль"
    "ru": (
        "января",
        "февраля",
        "марта",
        "апреля",
        "мая",
        "июня",
        "июля",
        "августа",
        "сентября",
        "октября",
        "ноября",
        "декабря",
    ),
    "en": (
        "Jan",
        "Feb",
        "Mar",
        "Apr",
        "May",
        "Jun",
        "Jul",
        "Aug",
        "Sep",
        "Oct",
        "Nov",
        "Dec",
    ),
}


def format_day(value: str, language: str) -> str:
    """A date the way the member reads it: 25 июля for ru, Jul 25 for en."""
    try:
        parsed = date.fromisoformat(str(value)[:10])
    except ValueError:
        return str(value)
    month = MONTHS.get(language, MONTHS["ru"])[parsed.month - 1]
    return f"{parsed.day} {month}" if language == "ru" else f"{month} {parsed.day}"


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


TITLES = {
    "ru": {
        "comment": ("Новый комментарий", "под вашей тренировкой"),
        "reply": ("Ответ на ваш комментарий", ""),
        "vote_request": ("Нужен ваш голос", "Тренировка ждёт подтверждения"),
        "vote_last_call": ("Голосование закрывается", "Осталось меньше трёх часов"),
        "approved": ("Тренировка зачтена", "ЗАЧТЕНО"),
        "rejected": ("Тренировка отклонена", "ОТКЛОНЕНО"),
        "buddy_credited": ("Совместная тренировка зачтена", "ЗАЧТЕНО"),
        "member_joined": ("Новый участник", "теперь тренируется с вами"),
        "member_left": ("Участник вышел", "покинул комнату"),
        "freeze_scheduled": ("Заморозка", ""),
        "freeze_canceled": ("Заморозка снята", "снова в деле"),
        "reminder": ("Период заканчивается", "Ещё есть время отметить тренировку"),
        "streak_at_risk": ("Серия под угрозой", "Одна тренировка спасёт серию"),
        "period_summary": ("Период закрыт", ""),
        "digest": ("Что произошло", "Собрали в одну сводку"),
        "welcome": ("BuddyGym на связи", "Буду присылать только важное"),
    },
    "en": {
        "comment": ("New comment", "under your workout"),
        "reply": ("Reply to your comment", ""),
        "vote_request": ("Your vote is needed", "A workout is waiting for approval"),
        "vote_last_call": ("Voting closes soon", "Less than three hours left"),
        "approved": ("Workout approved", "APPROVED"),
        "rejected": ("Workout rejected", "REJECTED"),
        "buddy_credited": ("Joint workout approved", "APPROVED"),
        "member_joined": ("New member", "now trains with you"),
        "member_left": ("Member left", "left the room"),
        "freeze_scheduled": ("Freeze", ""),
        "freeze_canceled": ("Freeze lifted", "back in the game"),
        "reminder": ("The period is ending", "There is still time to log a workout"),
        "streak_at_risk": ("Streak at risk", "One workout saves the streak"),
        "period_summary": ("Period closed", ""),
        "digest": ("What happened", "Folded into one summary"),
        "welcome": ("BuddyGym is here", "Only what matters"),
    },
}

CAPTIONS_EN = {
    "comment": "💬 New comment on your photo",
    "reply": "↩️ Reply to your comment",
    "vote_request": "🗳 Your vote is needed in \u00ab{room}\u00bb",
    "vote_last_call": "⏰ Voting closes soon",
    "approved": "✅ Workout approved in \u00ab{room}\u00bb",
    "rejected": "❌ Workout rejected in \u00ab{room}\u00bb",
    "buddy_credited": "🤝 Joint workout approved in \u00ab{room}\u00bb",
    "member_joined": "👋 New member in \u00ab{room}\u00bb",
    "member_left": "🚪 A member left \u00ab{room}\u00bb",
    "freeze_scheduled": "❄️ Freeze in \u00ab{room}\u00bb",
    "freeze_canceled": "🔥 Freeze lifted in \u00ab{room}\u00bb",
    "achievement": "🏅 New achievement: {achievement}",
    "reminder": "⏳ The period is ending",
    "streak_at_risk": "🔥 Streak at risk",
    "period_summary": "📊 Period summary",
    "digest": "📬 BuddyGym summary",
    "welcome": "👋 BuddyGym is here",
}

WELCOME_LINES = {
    "ru": (
        "Комментарии и ответы",
        "Голосования и зачёты",
        "Достижения",
        "Напоминание перед концом периода",
    ),
    "en": (
        "Comments and replies",
        "Votes and verdicts",
        "Achievements",
        "A nudge before the period ends",
    ),
}


def titles(kind: str, language: str) -> tuple[str, str]:
    table = TITLES.get(language, TITLES["ru"])
    return table.get(kind, (kind, ""))


def achievement_title(key: str) -> tuple[str, str]:
    return ACHIEVEMENTS.get(key, (key, "Новое достижение"))


def caption(kind: str, payload: dict[str, Any]) -> str:
    language = payload.get("language", "ru")
    source = CAPTIONS_EN if language == "en" else CAPTIONS
    template = source.get(kind, "BuddyGym")
    return template.format(
        actor=payload.get("actor_name", "Участник"),
        room=payload.get("room_name", "комнате"),
        achievement=achievement_title(payload.get("key", ""))[0],
    )


def _sport(payload: dict[str, Any]) -> str:
    """The member's own discipline, shown only when they picked one."""
    emoji = payload.get("sport_emoji", "")
    name = payload.get("sport_name", "")
    return f"{emoji} {name}".strip() if (emoji or name) else ""


def _lines(payload: dict[str, Any]) -> list[tuple[str, int, int]]:
    return [tuple(line) for line in payload.get("lines", [])]


def card_for(kind: str, payload: dict[str, Any]) -> CardData:
    language = payload.get("language", "ru")
    room = payload.get("room_name", "")
    actor = payload.get("actor_name", "")
    title, second = titles(kind, language)

    match kind:
        case "comment" | "reply":
            return CardData(
                title=title,
                subtitle=second,
                room_name=room,
                actor_name=actor,
                body=payload.get("body", ""),
                quote_author=payload.get("reply_to_author", ""),
                quote_body=payload.get("reply_to_body", ""),
            )
        case "vote_request" | "vote_last_call":
            return CardData(
                title=title,
                subtitle=second,
                room_name=room,
                actor_name=actor,
                accent=AMBER if kind == "vote_last_call" else GREEN_DEEP,
                votes_have=int(payload.get("votes_approve", 0)),
                votes_need=int(payload.get("votes_required", 1)),
                footer="Голосов на зачёт" if language == "ru" else "Votes to approve",
            )
        case "approved" | "buddy_credited":
            return CardData(
                title=title,
                badge=second,
                room_name=room,
                accent=GREEN_DEEP,
                done=int(payload.get("done", 0)),
                goal=int(payload.get("goal", 0)),
                footer=_sport(payload),
            )
        case "rejected":
            return CardData(
                title=title,
                badge=second,
                room_name=room,
                accent=RED,
                done=int(payload.get("done", 0)),
                goal=int(payload.get("goal", 0)),
                footer=_sport(payload),
            )
        case "member_joined":
            return CardData(title=title, room_name=room, actor_name=actor, footer=second)
        case "member_left":
            return CardData(
                title=title, room_name=room, actor_name=actor, accent=INK_SOFT, footer=second
            )
        case "freeze_scheduled":
            starts = format_day(payload.get("starts_at", ""), language)
            ends = format_day(payload.get("ends_at", ""), language)
            span = f"с {starts} по {ends}" if language == "ru" else f"{starts} to {ends}"
            return CardData(
                title=title, room_name=room, actor_name=actor, accent=PURPLE, footer=span
            )
        case "freeze_canceled":
            return CardData(
                title=title, room_name=room, actor_name=actor, accent=GREEN_DEEP, footer=second
            )
        case "achievement":
            name, subtitle = achievement_title(payload.get("key", ""))
            return CardData(title=name, subtitle=subtitle, room_name=room, accent=GREEN_DEEP)
        case "reminder" | "streak_at_risk" | "period_summary":
            return CardData(
                title=title,
                subtitle=payload.get("subtitle", ""),
                accent=AMBER if kind == "streak_at_risk" else GREEN_DEEP,
                lines=_lines(payload),
                footer=payload.get("footer", second),
            )
        case "digest":
            return CardData(
                title=title, subtitle=payload.get("subtitle", second), lines=_lines(payload)
            )
        case "welcome":
            lines = WELCOME_LINES.get(language, WELCOME_LINES["ru"])
            return CardData(title=title, subtitle=second, lines=[(line, 0, 0) for line in lines])
        case _:
            return CardData(title="BuddyGym", room_name=room)
