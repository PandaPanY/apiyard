package mock

import "testing"

func TestEscapeQuotes(t *testing.T) {
	if got := escapeQuotes("a'b" + string(rune(92)) + "c"); got != "a"+string(rune(92))+"'b"+string(rune(92))+string(rune(92))+"c" {
		t.Errorf("得到 %s", got)
	}
	if got := propertyPath("x-y"); got != "['x-y']" {
		t.Errorf("得到 %s", got)
	}
}
