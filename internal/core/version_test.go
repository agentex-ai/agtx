package core

import "testing"

func TestCompareVersionNumericOrdering(t *testing.T) {
	if compareVersion("1.10.0", "1.2.0") <= 0 {
		t.Fatal("expected 1.10.0 to sort after 1.2.0")
	}
	if compareVersion("v2.0.0", "10.0.0") >= 0 {
		t.Fatal("expected v2.0.0 to sort before 10.0.0")
	}
}
