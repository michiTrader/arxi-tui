package main

import (
	"strings"
	"unicode"
)

// A light turn is a message that needs nothing but a reply: "hola", "ping", "what model
// are you". Before this every message, however small, carried the whole standing load of
// a working session: the tool schemas and their hints (about 1 KB) when a project folder
// was open, the interface tools plus a 22 KB guide once /ui had been said, the history,
// and the thinking level. A user testing a provider with "ping" paid for all of it and
// got an answer about the project.
//
// A light turn carries none of it: no tools, no project rules, no history, no thinking,
// no interface guide, only the one-sentence system prompt. It is deliberately a short
// list of whole messages rather than a rule about length or words: "arregla eso" and
// "reintenta" are as short as "hola" and need everything. A message that is not on the
// list (or has anything more in it) is an ordinary turn. The list is the user's: the
// text keys chat.light (yes/no) and chat.light.phrases change it with /ui text or ui_edit.

// lightPhrasesDefault are messages that need no context to be answered, in the two
// languages the app is written in. "ok" and "listo" are NOT here: they answer a question
// the model asked, so they need the conversation.
const lightPhrasesDefault = "hola, buenas, buenos dias, buenas tardes, buenas noches, que tal, como estas, " +
	"gracias, muchas gracias, adios, chao, hasta luego, ping, pong, test, prueba, probando, " +
	"que modelo eres, que modelo es, que modelo usas, quien eres, como te llamas, " +
	"hi, hello, hey, thanks, thank you, thx, bye, goodbye, how are you, testing, " +
	"what model are you, which model are you, what model is this, who are you, what is your name"

// lightKey is how a message is compared: lower case, no accents, no punctuation, single
// spaces. "¿Qué modelo eres?" and "que modelo eres" are the same message.
func lightKey(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'á', 'à', 'ä', 'â':
			r = 'a'
		case 'é', 'è', 'ë', 'ê':
			r = 'e'
		case 'í', 'ì', 'ï', 'î':
			r = 'i'
		case 'ó', 'ò', 'ö', 'ô':
			r = 'o'
		case 'ú', 'ù', 'ü', 'û':
			r = 'u'
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// isLightMessage reports whether text is, whole, one of the phrases (a comma separated
// list) and light turns are on. A message that merely starts with one ("hola, arregla el
// banner") is an ordinary turn.
func isLightMessage(text, phrases string, on bool) bool {
	if !on {
		return false
	}
	key := lightKey(text)
	if key == "" || len(text) > 200 {
		return false
	}
	for _, p := range strings.Split(phrases, ",") {
		if lightKey(p) == key {
			return true
		}
	}
	return false
}

// lightTurnsOn reads the user's settings: are light turns on, and which phrases.
func lightTurnsOn() (on bool, phrases string) {
	return !isNo(uiText("chat.light")), uiText("chat.light.phrases")
}
