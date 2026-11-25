package probe

import "strings"

// language is one entry of the language table: the ISO 639-2/B code Popcorn
// stores, the ISO 639-1 code, the 639-2/T variant where it differs, and the
// English name.
type language struct {
	code, short, alt, name string
}

var languages = []language{
	{"eng", "en", "", "English"},
	{"hin", "hi", "", "Hindi"},
	{"kan", "kn", "", "Kannada"},
	{"tam", "ta", "", "Tamil"},
	{"tel", "te", "", "Telugu"},
	{"mal", "ml", "", "Malayalam"},
	{"mar", "mr", "", "Marathi"},
	{"ben", "bn", "", "Bengali"},
	{"guj", "gu", "", "Gujarati"},
	{"pan", "pa", "", "Punjabi"},
	{"urd", "ur", "", "Urdu"},
	{"ori", "or", "", "Odia"},
	{"jpn", "ja", "", "Japanese"},
	{"kor", "ko", "", "Korean"},
	{"chi", "zh", "zho", "Chinese"},
	{"tha", "th", "", "Thai"},
	{"vie", "vi", "", "Vietnamese"},
	{"ind", "id", "", "Indonesian"},
	{"may", "ms", "msa", "Malay"},
	{"fil", "tl", "tgl", "Filipino"},
	{"spa", "es", "", "Spanish"},
	{"fre", "fr", "fra", "French"},
	{"ger", "de", "deu", "German"},
	{"ita", "it", "", "Italian"},
	{"por", "pt", "", "Portuguese"},
	{"dut", "nl", "nld", "Dutch"},
	{"rus", "ru", "", "Russian"},
	{"ukr", "uk", "", "Ukrainian"},
	{"pol", "pl", "", "Polish"},
	{"cze", "cs", "ces", "Czech"},
	{"slo", "sk", "slk", "Slovak"},
	{"hun", "hu", "", "Hungarian"},
	{"rum", "ro", "ron", "Romanian"},
	{"bul", "bg", "", "Bulgarian"},
	{"gre", "el", "ell", "Greek"},
	{"tur", "tr", "", "Turkish"},
	{"ara", "ar", "", "Arabic"},
	{"heb", "he", "", "Hebrew"},
	{"per", "fa", "fas", "Persian"},
	{"swe", "sv", "", "Swedish"},
	{"nor", "no", "", "Norwegian"},
	{"nob", "nb", "", "Norwegian Bokmål"},
	{"dan", "da", "", "Danish"},
	{"fin", "fi", "", "Finnish"},
	{"ice", "is", "isl", "Icelandic"},
	{"hrv", "hr", "", "Croatian"},
	{"srp", "sr", "", "Serbian"},
	{"slv", "sl", "", "Slovenian"},
	{"est", "et", "", "Estonian"},
	{"lav", "lv", "", "Latvian"},
	{"lit", "lt", "", "Lithuanian"},
	{"cat", "ca", "", "Catalan"},
	{"baq", "eu", "eus", "Basque"},
	{"glg", "gl", "", "Galician"},
	{"nep", "ne", "", "Nepali"},
	{"sin", "si", "", "Sinhala"},
	{"swa", "sw", "", "Swahili"},
	{"afr", "af", "", "Afrikaans"},
}

var (
	byKey  = map[string]*language{}
	byCode = map[string]*language{}
)

func init() {
	for i := range languages {
		l := &languages[i]
		byCode[l.code] = l
		for _, k := range []string{l.code, l.short, l.alt, strings.ToLower(l.name)} {
			if k != "" {
				byKey[k] = l
			}
		}
	}
	// Common aliases seen in file names and stream tags.
	alias := map[string]string{
		"english": "eng", "hindi": "hin", "japanese": "jpn", "jap": "jpn",
		"chs": "chi", "cht": "chi", "cmn": "chi", "yue": "chi", "mandarin": "chi", "cantonese": "chi",
		"pt-br": "por", "pob": "por", "brazilian": "por", "es-419": "spa", "latino": "spa",
		"castellano": "spa", "deutsch": "ger", "francais": "fre", "français": "fre",
		"espanol": "spa", "español": "spa", "italiano": "ita", "nihongo": "jpn",
		"tagalog": "fil", "farsi": "per", "bokmal": "nob", "nno": "nor", "nn": "nor",
	}
	for k, code := range alias {
		byKey[k] = byCode[code]
	}
}

// NormalizeLanguage maps a language code or English name ("en", "eng",
// "English", "pt-BR") to the ISO 639-2/B code Popcorn stores. Unknown
// three-letter codes are returned lower-cased; anything else unknown gives "".
func NormalizeLanguage(s string) string {
	k := strings.ToLower(strings.TrimSpace(s))
	if k == "" || k == "und" {
		return ""
	}
	if l, ok := byKey[k]; ok {
		return l.code
	}
	if i := strings.IndexAny(k, "-_"); i > 0 {
		if l, ok := byKey[k[:i]]; ok {
			return l.code
		}
	}
	if len(k) == 3 && isLetters(k) {
		return k
	}
	return ""
}

// IsLanguage reports whether s names a known language, as a code or a name.
// File-name parsing uses it to tell "Movie.en.srt" from "Movie.final.srt".
func IsLanguage(s string) bool {
	_, ok := byKey[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

// LanguageName returns the English name of a language code, or the code.
func LanguageName(code string) string {
	if l, ok := byKey[strings.ToLower(code)]; ok {
		return l.name
	}
	if code == "" {
		return "Unknown"
	}
	return code
}

// ShortLanguage returns the ISO 639-1 code (used for WebVTT srclang and TMDB
// requests), or the input when there is none.
func ShortLanguage(code string) string {
	if l, ok := byKey[strings.ToLower(code)]; ok && l.short != "" {
		return l.short
	}
	return code
}

func isLetters(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}
