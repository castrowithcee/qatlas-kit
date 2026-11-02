package tui

// RuleKeys are the key names the rules of a binding spell, for the key literal test of package tui_test.
func RuleKeys() []string {
	var keys []string
	for _, set := range []map[string]bool{captureExits, freeCtrl} {
		for k := range set {
			keys = append(keys, k)
		}
	}
	for _, set := range []map[string]string{takenKeys, fixedKeys, arrowGlyphs} {
		for k := range set {
			keys = append(keys, k)
		}
	}
	return keys
}
