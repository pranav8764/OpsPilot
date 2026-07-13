package server

import "testing"

func TestProjectLimitsFailClosed(t *testing.T) {
	if projectLimit("free") != 3 || projectLimit("pro") != 25 || projectLimit("team") != 50 {
		t.Fatal("known project plan limits are incorrect")
	}
	if projectLimit("unknown") != 3 {
		t.Fatal("unknown plans must receive the free-plan limit")
	}
}
