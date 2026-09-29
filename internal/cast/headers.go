package cast

import (
	"sort"
	"strings"
)

// inputHeaderArgs is the -headers option for the next -i: the referrer, then
// every other header the provider named, in a stable order.
//
// A Referer among the headers gives way to the referrer, which is the one the
// playback path already settled on; it is used only when there is no referrer.
func inputHeaderArgs(referrer string, headers map[string]string) []string {
	var block strings.Builder
	referrer = strings.TrimSpace(referrer)

	values := make(map[string]string, len(headers))
	names := make([]string, 0, len(headers))
	for name, value := range headers {
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		if strings.EqualFold(name, "Referer") {
			if referrer == "" {
				referrer = value
			}
			continue
		}
		names = append(names, name)
		values[name] = value
	}
	sort.Strings(names)

	if referrer != "" {
		block.WriteString("Referer: " + referrer + "\r\n")
	}
	for _, name := range names {
		block.WriteString(name + ": " + values[name] + "\r\n")
	}
	if block.Len() == 0 {
		return nil
	}
	return []string{"-headers", block.String()}
}
