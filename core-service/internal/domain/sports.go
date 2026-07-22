package domain

// SportEmojis is the full Unicode sport and activity set the picker offers.
// The frontend list must stay identical, including variation selectors.
var sportEmojiList = []string{
	"⚽", "🏀", "🏈", "⚾", "🥎", "🎾", "🏐", "🏉", "🥏", "🎱",
	"🏓", "🏸", "🏒", "🏑", "🥍", "🏏", "🎳", "🥅", "⛳", "🪁",
	"🏹", "🎣", "🤿", "🥊", "🥋", "🎽", "🛹", "🛼", "🛷", "⛸️",
	"🥌", "🎿", "⛷️", "🏂", "🪂", "🏋️", "🤼", "🤸", "⛹️", "🤺",
	"🤾", "🏌️", "🏇", "🧘", "🏄", "🏊", "🤽", "🚣", "🧗", "🚵",
	"🚴", "🏃", "🚶", "🥾", "💃", "🕺", "🩰", "🤹", "🎯", "♟️",
	"🪃", "🏆", "🥇", "🥈", "🥉", "🏅",
}

var sportEmojiSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(sportEmojiList))
	for _, e := range sportEmojiList {
		m[e] = struct{}{}
	}
	return m
}()

func IsSportEmoji(s string) bool {
	_, ok := sportEmojiSet[s]
	return ok
}
