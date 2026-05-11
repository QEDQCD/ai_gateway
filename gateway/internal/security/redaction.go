package security

import "regexp"

var (
	phonePattern  = regexp.MustCompile(`\b(1[3-9]\d)\d{4}(\d{4})\b`)
	idCardPattern = regexp.MustCompile(`\b(\d{6})\d{8}([0-9Xx]{4})\b`)
	emailPattern  = regexp.MustCompile(`\b([A-Za-z0-9._%+-])([A-Za-z0-9._%+-]*)([A-Za-z0-9._%+-])@([A-Za-z0-9.-]+\.[A-Za-z]{2,})\b`)
)

func RedactText(input string) string {
	return RedactTextForDisplay(input)
}

func RedactTextForDisplay(input string) string {
	if input == "" {
		return ""
	}

	output := phonePattern.ReplaceAllString(input, `${1}XXXX${2}`)
	output = idCardPattern.ReplaceAllString(output, `${1}XXXXXX${2}`)
	output = emailPattern.ReplaceAllStringFunc(output, func(value string) string {
		matches := emailPattern.FindStringSubmatch(value)
		if len(matches) != 5 {
			return value
		}
		return matches[1] + "***" + matches[3] + "@" + matches[4]
	})
	return output
}

func SanitizeTextForUpstream(input string) string {
	if input == "" {
		return ""
	}

	output := phonePattern.ReplaceAllString(input, `***`)
	output = idCardPattern.ReplaceAllString(output, `***`)
	output = emailPattern.ReplaceAllStringFunc(output, func(string) string {
		return "***"
	})
	return output
}
